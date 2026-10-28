// Package house keeps physical addresses separate from normalized photo geometry.
package house

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Run struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Output   string `json:"output"`
	Start    int    `json:"start"`
	Count    int    `json:"count"`
	MirrorOf string `json:"mirror_of,omitempty"`
}
type Adjustment struct {
	Index int   `json:"index"`
	Point Point `json:"point"`
}
type Mapping struct {
	RunID       string       `json:"run_id"`
	Points      []Point      `json:"points"`
	Reverse     bool         `json:"reverse"`
	Adjustments []Adjustment `json:"adjustments"`
}
type View struct {
	PhotoID  string    `json:"photo_id"`
	Turns    int       `json:"turns"`
	Mappings []Mapping `json:"mappings"`
}
type Layout struct {
	Schema int    `json:"schema"`
	Runs   []Run  `json:"runs"`
	Views  []View `json:"views"`
}

func Default() Layout { return Layout{Schema: 1, Runs: []Run{}, Views: []View{}} }

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (p Point) valid() bool {
	return !math.IsNaN(p.X) && !math.IsNaN(p.Y) && p.X >= 0 && p.X <= 1 && p.Y >= 0 && p.Y <= 1
}
func (l Layout) Validate() error {
	if l.Runs == nil || l.Views == nil {
		return errors.New("runs and views must be arrays")
	}
	if l.Schema != 1 || len(l.Runs) > 40 || len(l.Views) > 20 {
		return errors.New("house requires schema 1, at most 40 runs and 20 photo views")
	}
	runs := map[string]Run{}
	total := 0
	for _, r := range l.Runs {
		if !identifier.MatchString(r.ID) || !identifier.MatchString(r.Output) || strings.TrimSpace(r.Name) == "" || utf8.RuneCountInString(r.Name) > 80 || r.Count < 1 || r.Count > 2048 || r.Start < 1 || r.Start > 2049-r.Count {
			return errors.New("run needs an ID, name, output and addresses within 1–2048")
		}
		if _, ok := runs[r.ID]; ok {
			return errors.New("run IDs must be unique")
		}
		for _, old := range runs {
			if old.Output == r.Output && r.Start < old.Start+old.Count && old.Start < r.Start+r.Count {
				return fmt.Errorf("address ranges overlap on output %s", r.Output)
			}
		}
		total += r.Count
		if total > 8192 {
			return errors.New("use at most 8192 bulbs across all runs including mirrors")
		}
		runs[r.ID] = r
	}
	for _, r := range l.Runs {
		if r.MirrorOf != "" {
			source, ok := runs[r.MirrorOf]
			if !ok || source.ID == r.ID || source.MirrorOf != "" || source.Count != r.Count {
				return errors.New("a mirrored run must name a non-mirrored source with the same bulb count")
			}
		}
	}
	photos := map[string]bool{}
	for _, v := range l.Views {
		if v.Mappings == nil {
			return errors.New("view mappings must be an array")
		}
		if !identifier.MatchString(v.PhotoID) || photos[v.PhotoID] || v.Turns < 0 || v.Turns > 3 || len(v.Mappings) > 40 {
			return errors.New("photo views require unique IDs, 0–3 turns and at most 40 mappings")
		}
		photos[v.PhotoID] = true
		mapped := map[string]bool{}
		for _, m := range v.Mappings {
			if m.Adjustments == nil {
				return errors.New("bulb adjustments must be an array")
			}
			run, ok := runs[m.RunID]
			if !ok || mapped[m.RunID] || len(m.Points) < 2 || len(m.Points) > 100 || len(m.Adjustments) > run.Count {
				return errors.New("each mapping needs a unique existing run and 2–100 roofline points")
			}
			mapped[m.RunID] = true
			length := 0.0
			for i, p := range m.Points {
				if !p.valid() {
					return errors.New("coordinates must be finite and between 0 and 1")
				}
				if i > 0 {
					length += math.Hypot(p.X-m.Points[i-1].X, p.Y-m.Points[i-1].Y)
				}
			}
			if length < 0.000001 {
				return errors.New("roofline must have nonzero length")
			}
			indices := map[int]bool{}
			for _, a := range m.Adjustments {
				if a.Index < 0 || a.Index >= run.Count || indices[a.Index] || !a.Point.valid() {
					return errors.New("bulb adjustments need unique valid indices and coordinates")
				}
				indices[a.Index] = true
			}
		}
	}
	return nil
}

// Positions returns physical-index order, using pixel aspect ratio for distance.
func Positions(m Mapping, count int, width, height float64) []Point {
	if count < 1 || count > 2048 || len(m.Points) < 2 || width <= 0 || height <= 0 {
		return nil
	}
	distances := make([]float64, len(m.Points))
	for i := 1; i < len(m.Points); i++ {
		a, b := m.Points[i-1], m.Points[i]
		distances[i] = distances[i-1] + math.Hypot((b.X-a.X)*width, (b.Y-a.Y)*height)
	}
	total := distances[len(distances)-1]
	out := make([]Point, count)
	for i := range out {
		fraction := 0.5
		if count > 1 {
			fraction = float64(i) / float64(count-1)
		}
		if m.Reverse {
			fraction = 1 - fraction
		}
		target := total * fraction
		j := 1
		for j < len(distances)-1 && distances[j] < target {
			j++
		}
		a, b := m.Points[j-1], m.Points[j]
		t := 0.0
		if distances[j] > distances[j-1] {
			t = (target - distances[j-1]) / (distances[j] - distances[j-1])
		}
		out[i] = Point{a.X + (b.X-a.X)*t, a.Y + (b.Y-a.Y)*t}
	}
	for _, a := range m.Adjustments {
		if a.Index >= 0 && a.Index < count {
			out[a.Index] = a.Point
		}
	}
	return out
}
