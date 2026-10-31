package schedule

import (
	"errors"
	"fmt"
	sunrise "github.com/nathan-osman/go-sunrise"
	"reflect"
	"sort"
	"time"
)

// Issue is a preview notice. Skipped marks an occurrence that was dropped;
// callers MUST test it, never the wording of Message.
type Issue struct {
	RuleID  string `json:"rule_id"`
	Date    string `json:"date"`
	Message string `json:"message"`
	Skipped bool   `json:"skipped"`
}
type Candidate struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Priority int    `json:"priority"`
	Action   Action `json:"action"`
}
type Segment struct {
	From     time.Time   `json:"from"`
	Until    time.Time   `json:"until"`
	Action   Action      `json:"action"`
	Conflict bool        `json:"conflict"`
	Reason   string      `json:"reason"`
	Winners  []Candidate `json:"winners"`
	Shadowed []Candidate `json:"shadowed"`
}
type Preview struct {
	ExecutionEnabled bool      `json:"execution_enabled"`
	Timezone         string    `json:"timezone"`
	Segments         []Segment `json:"segments"`
	Issues           []Issue   `json:"issues"`
}
type occurrence struct {
	start, end time.Time
	candidate  Candidate
}

// Resolve clock boundaries independently of time.Date's unspecified choice at
// repeated or missing local times. Civil arithmetic always uses a UTC surrogate.
func clockBoundary(day time.Time, hhmm string, loc *time.Location, end bool) (time.Time, string, error) {
	clock, _ := time.Parse("15:04", hhmm)
	wall := time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, time.UTC)
	offsets := map[int]bool{}
	for h := -48; h <= 48; h += 6 {
		_, off := wall.Add(time.Duration(h) * time.Hour).In(loc).Zone()
		offsets[off] = true
	}
	matches := []time.Time{}
	var forward time.Time
	var delta time.Duration
	for off := range offsets {
		candidate := wall.Add(-time.Duration(off) * time.Second)
		local := candidate.In(loc)
		projected := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second(), 0, time.UTC)
		diff := projected.Sub(wall)
		if diff == 0 {
			matches = append(matches, candidate)
		}
		if diff > 0 && (forward.IsZero() || diff < delta) {
			forward = candidate
			delta = diff
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Before(matches[j]) })
	if len(matches) > 0 {
		if len(matches) == 1 {
			return matches[0], "", nil
		}
		if end {
			return matches[len(matches)-1], "Repeated clock time: using the last occurrence for the end.", nil
		}
		return matches[0], "Repeated clock time: using the first occurrence for the start.", nil
	}
	if !forward.IsZero() && delta <= 3*time.Hour {
		return forward, fmt.Sprintf("Missing clock time: shifted forward by %s.", delta), nil
	}
	return time.Time{}, "", errors.New("civil date or clock time does not exist")
}
func boundary(day time.Time, b Boundary, p Plan, loc *time.Location, end bool) (time.Time, string, error) {
	if b.Kind == "clock" {
		return clockBoundary(day, b.Time, loc, end)
	}
	// Search neighboring astronomical dates and require the un-offset event to
	// belong to this civil date (important near the International Date Line).
	for d := -2; d <= 2; d++ {
		base := day.AddDate(0, 0, d)
		rise, set := sunrise.SunriseSunset(p.Location.Latitude, p.Location.Longitude, base.Year(), base.Month(), base.Day())
		event := rise
		if b.Kind == "sunset" {
			event = set
		}
		if event.IsZero() {
			continue
		}
		local := event.In(loc)
		if local.Format("2006-01-02") == day.Format("2006-01-02") {
			return event.Round(time.Minute).Add(time.Duration(b.OffsetMinutes) * time.Minute), "", nil
		}
	}
	return time.Time{}, "", fmt.Errorf("no %s on this local date", b.Kind)
}
func includes(r Rule, day time.Time) bool {
	d := day.Format("2006-01-02")
	if r.From != "" && (d < r.From || d > r.Through) {
		return false
	}
	if r.Kind == "exception" {
		return true
	}
	for _, v := range r.Weekdays {
		if v == int(day.Weekday()) {
			return true
		}
	}
	return false
}
func rank(kind string) int {
	switch kind {
	case "override":
		return 4
	case "exception":
		return 3
	case "seasonal":
		return 2
	case "weekly":
		return 1
	}
	return 0
}
func choose(at time.Time, occurrences []occurrence) Segment {
	candidates := []Candidate{}
	for _, o := range occurrences {
		if !at.Before(o.start) && at.Before(o.end) {
			candidates = append(candidates, o.candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if rank(a.Kind) != rank(b.Kind) {
			return rank(a.Kind) > rank(b.Kind)
		}
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		return a.ID < b.ID
	})
	result := Segment{Action: Action{Kind: "off"}, Reason: "No active rule; default off.", Winners: []Candidate{}, Shadowed: []Candidate{}}
	if len(candidates) == 0 {
		return result
	}
	first := candidates[0]
	result.Action = first.Action
	result.Reason = "Highest active category and priority."
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		if rank(c.Kind) == rank(first.Kind) && c.Priority == first.Priority {
			result.Winners = append(result.Winners, c)
			if c.Action != first.Action {
				result.Conflict = true
			}
		} else {
			result.Shadowed = append(result.Shadowed, c)
		}
	}
	if result.Conflict {
		result.Action = Action{Kind: "off"}
		result.Reason = "Conflicting rules have equal precedence; proposed intent is off. Change a priority or time window."
	}
	return result
}
func sameIntent(a, b Segment) bool {
	a.From = time.Time{}
	a.Until = time.Time{}
	b.From = time.Time{}
	b.Until = time.Time{}
	return reflect.DeepEqual(a, b)
}

// Evaluate never stores state or connects to a controller. Call it with the
// current instant after restart; missed transitions are deliberately irrelevant.
func Evaluate(p Plan, from, until time.Time) (Preview, error) {
	out := Preview{Timezone: p.Timezone, Segments: []Segment{}, Issues: []Issue{}}
	if err := p.Validate(); err != nil {
		return out, err
	}
	if from.IsZero() || !until.After(from) || until.Sub(from) > 31*24*time.Hour || from.Year() < 1970 || until.Year() > 2199 {
		return out, errors.New("preview requires a positive range of at most 31 days between 1970 and 2199")
	}
	loc, _ := time.LoadLocation(p.Timezone)
	start := from.In(loc)
	finish := until.In(loc)
	day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -2)
	last := time.Date(finish.Year(), finish.Month(), finish.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	occurrences := []occurrence{}
	cuts := []time.Time{from.UTC(), until.UTC()}
	add := func(o occurrence) {
		if !o.end.After(from) || !o.start.Before(until) {
			return
		}
		occurrences = append(occurrences, o)
		if o.start.After(from) {
			cuts = append(cuts, o.start.UTC())
		}
		if o.end.Before(until) {
			cuts = append(cuts, o.end.UTC())
		}
	}
	for ; !day.After(last); day = day.AddDate(0, 0, 1) {
		for _, r := range p.Rules {
			if !r.Enabled || !includes(r, day) {
				continue
			}
			endDay := day
			if r.EndNextDay {
				endDay = endDay.AddDate(0, 0, 1)
			}
			a, an, ae := boundary(day, r.Start, p, loc, false)
			b, bn, be := boundary(endDay, r.End, p, loc, true)
			// Include notices for surrounding dates only when their occurrence intersects
			// this preview; failures on dates in the preview are always visible.
			inDate := endDay.Format("2006-01-02") >= start.Format("2006-01-02") && day.Format("2006-01-02") <= finish.Format("2006-01-02")
			intersects := ae == nil && be == nil && b.After(from) && a.Before(until)
			issue := func(message string, skipped bool) {
				if inDate || intersects {
					out.Issues = append(out.Issues, Issue{RuleID: r.ID, Date: day.Format("2006-01-02"), Message: message, Skipped: skipped})
				}
			}
			if ae != nil {
				issue("Skipped: "+ae.Error(), true)
				continue
			}
			if be != nil {
				issue("Skipped: "+be.Error(), true)
				continue
			}
			if !b.After(a) {
				issue("Skipped: end does not follow start on this date.", true)
				continue
			}
			if an != "" {
				issue(an, false)
			}
			if bn != "" {
				issue(bn, false)
			}
			add(occurrence{a, b, Candidate{r.ID, r.Name, r.Kind, r.Priority, r.Action}})
		}
	}
	if o := p.Override; o != nil {
		add(occurrence{o.StartsAt, o.ExpiresAt, Candidate{o.ID, "Temporary override", "override", 0, o.Action}})
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].Before(cuts[j]) })
	for i := 0; i < len(cuts)-1; i++ {
		if cuts[i].Equal(cuts[i+1]) {
			continue
		}
		s := choose(cuts[i], occurrences)
		s.From = cuts[i]
		s.Until = cuts[i+1]
		n := len(out.Segments)
		if n > 0 && sameIntent(out.Segments[n-1], s) {
			out.Segments[n-1].Until = s.Until
		} else {
			out.Segments = append(out.Segments, s)
		}
	}
	sort.Slice(out.Issues, func(i, j int) bool {
		a, b := out.Issues[i], out.Issues[j]
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		return a.Message < b.Message
	})
	return out, nil
}

// EvaluateDates expands local dates into absolute preview bounds on the server,
// so a phone in another timezone does not move the household's calendar day.
func EvaluateDates(p Plan, fromDate string, days int) (Preview, error) {
	if err := p.Validate(); err != nil {
		return Preview{}, err
	}
	day, err := date(fromDate)
	if err != nil {
		return Preview{}, err
	}
	if days < 1 || days > 30 {
		return Preview{}, errors.New("date preview supports 1–30 local days")
	}
	loc, _ := time.LoadLocation(p.Timezone)
	from, _, err := clockBoundary(day, "00:00", loc, false)
	if err != nil {
		return Preview{}, err
	}
	until, _, err := clockBoundary(day.AddDate(0, 0, days), "00:00", loc, false)
	if err != nil {
		return Preview{}, err
	}
	return Evaluate(p, from, until)
}
