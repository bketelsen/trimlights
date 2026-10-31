package schedule

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func instant(s string) time.Time {
	v, e := time.Parse(time.RFC3339, s)
	if e != nil {
		panic(e)
	}
	return v
}
func weekly() Rule {
	return Rule{ID: "weekly", Name: "Evenings", Enabled: true, Kind: "weekly", Weekdays: []int{0, 1, 2, 3, 4, 5, 6}, Start: Boundary{Kind: "clock", Time: "19:30"}, End: Boundary{Kind: "clock", Time: "22:59"}, Action: Action{Kind: "pattern", PatternID: "spooky", Revision: 1}}
}
func evaluate(t *testing.T, p Plan, from, until string) Preview {
	t.Helper()
	out, err := Evaluate(p, instant(from), instant(until))
	if err != nil {
		t.Fatal(err)
	}
	if out.ExecutionEnabled {
		t.Fatal("preview enabled execution")
	}
	return out
}
func TestWeeklyMatchesCapturedHouseholdWindow(t *testing.T) {
	p := Default()
	p.Rules = []Rule{weekly()}
	out := evaluate(t, p, "2026-10-28T23:00:00Z", "2026-10-29T04:00:00Z")
	if len(out.Segments) != 3 {
		t.Fatal(out)
	}
	on := out.Segments[1]
	if on.From != instant("2026-10-28T23:30:00Z") || on.Until != instant("2026-10-29T02:59:00Z") || on.Action.PatternID != "spooky" {
		t.Fatal(on)
	}
	if out.Segments[2].Action.Kind != "off" {
		t.Fatal(out)
	}
}
func TestDateRangesWeekdaysAndOvernightAnchor(t *testing.T) {
	p := Default()
	r := weekly()
	r.From = "2026-10-30"
	r.Through = "2026-10-30"
	r.Weekdays = []int{5}
	r.Start.Time = "23:00"
	r.End.Time = "01:00"
	r.EndNextDay = true
	p.Rules = []Rule{r}
	out := evaluate(t, p, "2026-10-31T04:30:00Z", "2026-11-01T06:00:00Z")
	if out.Segments[0].Action.Kind != "pattern" || out.Segments[0].Until != instant("2026-10-31T05:00:00Z") || len(out.Segments) != 2 {
		t.Fatal(out)
	}
}
func TestDSTBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, day, start, end, wantStart, wantEnd, notice string }{
		{"gap", "2026-03-08", "02:30", "04:00", "2026-03-08T07:30:00Z", "2026-03-08T08:00:00Z", "shifted forward by 1h0m0s"},
		{"repeat", "2026-11-01", "01:15", "01:45", "2026-11-01T05:15:00Z", "2026-11-01T06:45:00Z", "Repeated clock"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := Default()
			r := weekly()
			r.Kind = "exception"
			r.From = tc.day
			r.Through = tc.day
			r.Weekdays = nil
			r.Start.Time = tc.start
			r.End.Time = tc.end
			p.Rules = []Rule{r}
			out := evaluate(t, p, tc.day+"T00:00:00Z", tc.day+"T12:00:00Z")
			s := out.Segments[1]
			if s.From != instant(tc.wantStart) || s.Until != instant(tc.wantEnd) {
				t.Fatal(s)
			}
			if len(out.Issues) == 0 || !strings.Contains(out.Issues[0].Message, tc.notice) {
				t.Fatal(out.Issues)
			}
		})
	}
	// A 30-minute transition must not be treated as a one-hour gap.
	loc, _ := time.LoadLocation("Australia/Lord_Howe")
	day, _ := date("2026-10-04")
	got, notice, err := clockBoundary(day, "02:15", loc, false)
	if err != nil || got.In(loc).Format("15:04") != "02:45" || !strings.Contains(notice, "30m0s") {
		t.Fatal(got, notice, err)
	}
	loc, _ = time.LoadLocation("Pacific/Apia")
	day, _ = date("2011-12-30")
	if _, _, err = clockBoundary(day, "12:00", loc, false); err == nil {
		t.Fatal("skipped civil date was invented")
	}
}
func TestPrecedenceConflictAndRestart(t *testing.T) {
	p := Default()
	w := weekly()
	s := w
	s.ID = "seasonal"
	s.Name = "October"
	s.Kind = "seasonal"
	s.From = "2026-10-01"
	s.Through = "2026-10-31"
	s.Action.PatternID = "halloween"
	e := s
	e.ID = "exception"
	e.Name = "One night off"
	e.Kind = "exception"
	e.From = "2026-10-28"
	e.Through = e.From
	e.Weekdays = nil
	e.Action = Action{Kind: "off"}
	p.Rules = []Rule{w, s, e}
	p.Override = &Override{ID: "manual", Action: w.Action, StartsAt: instant("2026-10-29T00:00:00Z"), ExpiresAt: instant("2026-10-29T00:30:00Z")}
	out := evaluate(t, p, "2026-10-28T23:45:00Z", "2026-10-29T01:00:00Z")
	if len(out.Segments) != 3 || out.Segments[0].Winners[0].ID != "exception" || out.Segments[1].Winners[0].ID != "manual" || out.Segments[2].Action.Kind != "off" {
		t.Fatal(out)
	}
	restarted := evaluate(t, p, "2026-10-29T00:45:00Z", "2026-10-29T01:00:00Z")
	if !sameIntent(restarted.Segments[0], out.Segments[2]) {
		t.Fatal("restart replayed old intent")
	}
	p.Override = nil
	p.Rules = []Rule{w, s}
	tie := s
	tie.ID = "other"
	tie.Action.PatternID = "other"
	p.Rules = append(p.Rules, tie)
	out = evaluate(t, p, "2026-10-29T00:00:00Z", "2026-10-29T01:00:00Z")
	if !out.Segments[0].Conflict || out.Segments[0].Action.Kind != "off" || len(out.Segments[0].Winners) != 2 || len(out.Segments[0].Shadowed) != 1 {
		t.Fatal(out)
	}
	p.Rules = []Rule{tie, s, w}
	reverse := evaluate(t, p, "2026-10-29T00:00:00Z", "2026-10-29T01:00:00Z")
	if !reflect.DeepEqual(out, reverse) {
		t.Fatal("rule order changed the decision")
	}
	p.Rules[0].Priority = 1
	out = evaluate(t, p, "2026-10-29T00:00:00Z", "2026-10-29T01:00:00Z")
	if out.Segments[0].Conflict || out.Segments[0].Action.PatternID != "other" {
		t.Fatal(out)
	}
	p.Rules[0].Priority = 0
	p.Rules[0].Action = s.Action
	out = evaluate(t, p, "2026-10-29T00:00:00Z", "2026-10-29T01:00:00Z")
	if out.Segments[0].Conflict || len(out.Segments[0].Winners) != 2 {
		t.Fatal(out)
	}
}
func TestSolarOffsetsPolarAndDateLine(t *testing.T) {
	p := Default()
	p.Location = &Location{40.7128, -74.006}
	r := weekly()
	r.Start = Boundary{Kind: "sunset", OffsetMinutes: -30}
	r.End.Time = "23:00"
	p.Rules = []Rule{r}
	out := evaluate(t, p, "2026-06-21T18:00:00Z", "2026-06-22T04:00:00Z")
	start := out.Segments[1].From
	// NYC June solstice sunset about 20:31 EDT; start about 20:01 EDT.
	if start.Before(instant("2026-06-21T23:56:00Z")) || start.After(instant("2026-06-22T00:06:00Z")) {
		t.Fatal(start)
	}
	p.Timezone = "Arctic/Longyearbyen"
	p.Location = &Location{78.2232, 15.6469}
	out = evaluate(t, p, "2026-06-21T00:00:00Z", "2026-06-22T00:00:00Z")
	if len(out.Segments) != 1 || out.Segments[0].Action.Kind != "off" || len(out.Issues) == 0 || !strings.Contains(out.Issues[0].Message, "no sunset") {
		t.Fatal(out)
	}
	p.Timezone = "Pacific/Kiritimati"
	p.Location = &Location{1.8721, -157.4278}
	loc, _ := time.LoadLocation(p.Timezone)
	day, _ := date("2026-06-21")
	sunset, _, err := boundary(day, Boundary{Kind: "sunset"}, p, loc, false)
	if err != nil || sunset.In(loc).Format("2006-01-02") != "2026-06-21" {
		t.Fatal(sunset, err)
	}
}
func TestInvalidPlans(t *testing.T) {
	for _, mutate := range []func(*Plan){
		func(p *Plan) { p.Timezone = "Local" }, func(p *Plan) { p.Timezone = "made/up" },
		func(p *Plan) { p.Rules[0].End.Time = "01:00" }, func(p *Plan) { p.Rules[0].Start.Time = "7:30" },
		func(p *Plan) { p.Rules[0].Weekdays = []int{0, 0} }, func(p *Plan) { p.Rules[0].Action.Revision = 0 },
		func(p *Plan) { p.Rules[0].Start = Boundary{Kind: "sunset"} }, func(p *Plan) { p.Rules[0].Kind = "exception" },
		func(p *Plan) { p.Rules[0].From = "2026-10-01" }, func(p *Plan) { p.Rules = append(p.Rules, p.Rules[0]) },
		func(p *Plan) {
			p.Override = &Override{ID: "manual", Action: Action{Kind: "off"}, StartsAt: instant("2026-01-01T00:00:00Z"), ExpiresAt: instant("2026-01-09T00:00:00Z")}
		},
	} {
		p := Default()
		p.Rules = []Rule{weekly()}
		mutate(&p)
		if p.Validate() == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
	if _, err := Evaluate(Default(), instant("2026-01-01T00:00:00Z"), instant("2026-02-02T00:00:00Z")); err == nil {
		t.Fatal("unbounded preview")
	}
}

func TestLocalDatePreviewUsesDSTDayLength(t *testing.T) {
	p := Default()
	for _, tc := range []struct {
		date  string
		hours int
	}{{"2026-03-08", 23}, {"2026-11-01", 25}} {
		out, err := EvaluateDates(p, tc.date, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Segments) != 1 || out.Segments[0].Until.Sub(out.Segments[0].From) != time.Duration(tc.hours)*time.Hour {
			t.Fatal(out)
		}
	}
	if _, err := EvaluateDates(p, "2026-02-30", 1); err == nil {
		t.Fatal("invalid local date")
	}
	if _, err := EvaluateDates(p, "2026-01-01", 31); err == nil {
		t.Fatal("invalid local day count")
	}
}
func TestGapInvertsIntervalWithVisibleSkip(t *testing.T) {
	p := Default()
	r := weekly()
	r.Start.Time = "02:30"
	r.End.Time = "03:15"
	p.Rules = []Rule{r}
	out := evaluate(t, p, "2026-03-08T05:00:00Z", "2026-03-09T04:00:00Z")
	if len(out.Segments) != 1 || out.Segments[0].Action.Kind != "off" || len(out.Issues) != 1 || !strings.Contains(out.Issues[0].Message, "end does not follow start") {
		t.Fatal(out)
	}
}
func TestSolarGoldenAndOffsetAcrossMidnight(t *testing.T) {
	p := Default()
	p.Timezone = "America/Toronto"
	p.Location = &Location{43.65, -79.38}
	loc, _ := time.LoadLocation(p.Timezone)
	day, _ := date("2000-01-01")
	rise, _, err := boundary(day, Boundary{Kind: "sunrise"}, p, loc, false)
	// Upstream's published Toronto golden: 12:51:00 UTC, rounded to the minute.
	if err != nil || !rise.Equal(instant("2000-01-01T12:51:00Z")) {
		t.Fatal(rise, err)
	}
	p.Timezone = "Europe/Oslo"
	p.Location = &Location{63.4305, 10.3951}
	loc, _ = time.LoadLocation(p.Timezone)
	day, _ = date("2026-06-21")
	set, _, err := boundary(day, Boundary{Kind: "sunset", OffsetMinutes: 180}, p, loc, false)
	if err != nil || set.In(loc).Format("2006-01-02") != "2026-06-22" {
		t.Fatal(set, err)
	}
}

// Callers rely on Issue.Skipped, not message wording. Cover every skip source
// and confirm that adjustment notices for kept occurrences are not skips.
func TestIssueSkippedIsTypedForEverySource(t *testing.T) {
	polar := func(start, end Boundary) Plan {
		p := Default()
		p.Timezone = "Arctic/Longyearbyen"
		p.Location = &Location{78.2232, 15.6469}
		r := weekly()
		r.Start, r.End = start, end
		p.Rules = []Rule{r}
		return p
	}
	clock := func(day, start, end, tz string) Plan {
		p := Default()
		p.Timezone = tz
		r := weekly()
		r.Kind, r.From, r.Through, r.Weekdays = "exception", day, day, nil
		r.Start.Time, r.End.Time = start, end
		p.Rules = []Rule{r}
		return p
	}
	for _, tc := range []struct {
		name        string
		plan        Plan
		from, until string
		skipped     bool
	}{
		{"start-boundary", polar(Boundary{Kind: "sunset"}, Boundary{Kind: "clock", Time: "23:00"}), "2026-06-21T00:00:00Z", "2026-06-22T00:00:00Z", true},
		{"end-boundary", polar(Boundary{Kind: "clock", Time: "18:00"}, Boundary{Kind: "sunset"}), "2026-06-21T00:00:00Z", "2026-06-22T00:00:00Z", true},
		{"missing-civil-date", clock("2011-12-30", "12:00", "13:00", "Pacific/Apia"), "2011-12-29T00:00:00Z", "2011-12-31T12:00:00Z", true},
		{"inverted", clock("2026-03-08", "02:30", "03:15", "America/New_York"), "2026-03-08T05:00:00Z", "2026-03-09T04:00:00Z", true},
		{"gap-shift", clock("2026-03-08", "02:30", "04:00", "America/New_York"), "2026-03-08T00:00:00Z", "2026-03-08T12:00:00Z", false},
		{"repeat", clock("2026-11-01", "01:15", "01:45", "America/New_York"), "2026-11-01T00:00:00Z", "2026-11-01T12:00:00Z", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := evaluate(t, tc.plan, tc.from, tc.until)
			if len(out.Issues) == 0 {
				t.Fatal("no issue", out)
			}
			for _, issue := range out.Issues {
				if issue.Skipped != tc.skipped {
					t.Fatal(issue)
				}
			}
		})
	}
}
