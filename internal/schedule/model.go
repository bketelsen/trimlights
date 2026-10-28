// Package schedule evaluates household calendar intent without hardware access.
package schedule

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // Required by the scratch image, including DST transitions.
	"unicode"
	"unicode/utf8"
)

type Action struct {
	Kind      string `json:"kind"`
	PatternID string `json:"pattern_id,omitempty"`
	Revision  int    `json:"revision,omitempty"`
}
type Boundary struct {
	Kind          string `json:"kind"`
	Time          string `json:"time,omitempty"`
	OffsetMinutes int    `json:"offset_minutes,omitempty"`
}
type Rule struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Enabled    bool     `json:"enabled"`
	Kind       string   `json:"kind"`
	Priority   int      `json:"priority"`
	From       string   `json:"from,omitempty"`
	Through    string   `json:"through,omitempty"`
	Weekdays   []int    `json:"weekdays"`
	Start      Boundary `json:"start"`
	End        Boundary `json:"end"`
	EndNextDay bool     `json:"end_next_day"`
	Action     Action   `json:"action"`
}
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}
type Override struct {
	ID        string    `json:"id"`
	Action    Action    `json:"action"`
	StartsAt  time.Time `json:"starts_at"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Plan struct {
	Schema   int       `json:"schema"`
	Timezone string    `json:"timezone"`
	Location *Location `json:"location,omitempty"`
	Rules    []Rule    `json:"rules"`
	Override *Override `json:"override,omitempty"`
}

func Default() Plan { return Plan{Schema: 1, Timezone: "America/New_York", Rules: []Rule{}} }
func (a Action) Validate() error {
	if a.Kind == "off" && a.PatternID == "" && a.Revision == 0 {
		return nil
	}
	if a.Kind == "pattern" && validID(a.PatternID) && a.Revision > 0 {
		return nil
	}
	return errors.New("action must be off or reference a saved pattern revision")
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

func validID(s string) bool { return idPattern.MatchString(s) }
func validName(s string) bool {
	if strings.TrimSpace(s) != s || s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > 80 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func date(s string) (time.Time, error) {
	d, err := time.Parse("2006-01-02", s)
	if err != nil || len(s) != 10 || d.Year() < 1970 || d.Year() > 2199 {
		return time.Time{}, errors.New("dates must use YYYY-MM-DD between 1970 and 2199")
	}
	return d, nil
}
func (b Boundary) validate(location *Location) error {
	switch b.Kind {
	case "clock":
		_, err := time.Parse("15:04", b.Time)
		if err != nil || len(b.Time) != 5 || b.OffsetMinutes != 0 {
			return errors.New("clock boundary must use HH:MM without an offset")
		}
	case "sunrise", "sunset":
		if b.Time != "" || b.OffsetMinutes < -180 || b.OffsetMinutes > 180 || location == nil {
			return errors.New("solar boundary requires location and an offset between -180 and 180 minutes")
		}
	default:
		return errors.New("boundary must be clock, sunrise or sunset")
	}
	return nil
}
func (p Plan) Validate() error {
	if p.Schema != 1 {
		return errors.New("unsupported calendar schema")
	}
	if p.Timezone == "" || p.Timezone == "Local" {
		return errors.New("an explicit IANA timezone is required")
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		return errors.New("unknown IANA timezone")
	}
	if p.Location != nil {
		a, b := p.Location.Latitude, p.Location.Longitude
		if math.IsNaN(a) || math.IsInf(a, 0) || math.IsNaN(b) || math.IsInf(b, 0) || a < -90 || a > 90 || b < -180 || b > 180 {
			return errors.New("invalid latitude or longitude")
		}
	}
	if len(p.Rules) > 100 {
		return errors.New("a calendar supports at most 100 rules")
	}
	ids := map[string]bool{}
	for _, r := range p.Rules {
		if !validID(r.ID) || ids[r.ID] || !validName(r.Name) {
			return errors.New("rule IDs must be unique and names must contain 1–80 visible characters")
		}
		ids[r.ID] = true
		if r.Kind != "weekly" && r.Kind != "seasonal" && r.Kind != "exception" {
			return errors.New("invalid rule kind")
		}
		if r.Priority < 0 || r.Priority > 100 {
			return errors.New("priority must be 0–100")
		}
		if r.From != "" || r.Through != "" || r.Kind != "weekly" {
			from, err := date(r.From)
			if err != nil {
				return err
			}
			through, err := date(r.Through)
			if err != nil {
				return err
			}
			if through.Before(from) {
				return errors.New("date range ends before it starts")
			}
			if r.Kind == "exception" && r.From != r.Through {
				return errors.New("one-day exceptions require the same start and end date")
			}
		}
		if r.Kind == "exception" {
			if len(r.Weekdays) != 0 {
				return errors.New("exceptions must not specify weekdays")
			}
		} else {
			if len(r.Weekdays) == 0 {
				return errors.New("select at least one weekday")
			}
			seen := map[int]bool{}
			for _, d := range r.Weekdays {
				if d < 0 || d > 6 || seen[d] {
					return errors.New("weekdays must be distinct numbers from 0 through 6")
				}
				seen[d] = true
			}
		}
		if err := r.Start.validate(p.Location); err != nil {
			return fmt.Errorf("%s start: %w", r.Name, err)
		}
		if err := r.End.validate(p.Location); err != nil {
			return fmt.Errorf("%s end: %w", r.Name, err)
		}
		if !r.EndNextDay && r.Start.Kind == "clock" && r.End.Kind == "clock" && r.End.Time <= r.Start.Time {
			return errors.New("end must follow start; select next day for overnight windows")
		}
		if err := r.Action.Validate(); err != nil {
			return err
		}
	}
	if o := p.Override; o != nil {
		if !validID(o.ID) || ids[o.ID] {
			return errors.New("override must have a unique ID")
		}
		if o.StartsAt.IsZero() || o.ExpiresAt.IsZero() || !o.ExpiresAt.After(o.StartsAt) || o.ExpiresAt.Sub(o.StartsAt) > 7*24*time.Hour {
			return errors.New("override must have a positive duration of at most seven days")
		}
		if err := o.Action.Validate(); err != nil {
			return err
		}
	}
	return nil
}
