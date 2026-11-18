package storage

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
)

func TestCalendarInputsRejectsStaleRevision(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	p, err := s.CreatePattern(ctx, design(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	plan := calendar(p)
	saved, err := s.SaveSchedule(ctx, 1, plan, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CalendarInputs(ctx, saved.Revision-1, plan); !errors.Is(err, ErrScheduleConflict) {
		t.Fatal("stale base accepted", err)
	}
}

func TestCalendarInputsRetainsArchivedPinButRejectsNewRule(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	p, err := s.CreatePattern(ctx, design(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	plan := calendar(p)
	saved, err := s.SaveSchedule(ctx, 1, plan, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ArchivePattern(ctx, p.ID, p.Revision, true, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CalendarInputs(ctx, saved.Revision, plan); err != nil {
		t.Fatal("retained archived reference rejected", err)
	}
	duplicate := plan.Rules[0]
	duplicate.ID = "new-rule"
	plan.Rules = append(plan.Rules, duplicate)
	if _, err = s.CalendarInputs(ctx, saved.Revision, plan); !errors.Is(err, ErrScheduleReference) {
		t.Fatal("new archived reference accepted", err)
	}
}

func TestCalendarInputsReturnsOrderedHeadsAndPinnedRevision(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	p, err := s.CreatePattern(ctx, design(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	plan := calendar(p)
	saved, err := s.SaveSchedule(ctx, 1, plan, "owner")
	if err != nil {
		t.Fatal(err)
	}
	d := p.Design
	d.Name = "Updated"
	updated, err := s.UpdatePattern(ctx, p.ID, p.Revision, d, "owner")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreatePattern(ctx, design(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	archived, err := s.ArchivePattern(ctx, other.ID, other.Revision, true, "owner")
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.CalendarInputs(ctx, saved.Revision, plan)
	if err != nil {
		t.Fatal(err)
	}
	if out.Revision != saved.Revision {
		t.Fatalf("revision = %d, want %d", out.Revision, saved.Revision)
	}
	want := []SavedPattern{updated, archived}
	sort.Slice(want, func(i, j int) bool { return want[i].ID < want[j].ID })
	if !reflect.DeepEqual(out.Heads, want) {
		t.Fatalf("heads = %+v, want %+v", out.Heads, want)
	}
	if len(out.Pinned) != 1 || !reflect.DeepEqual(out.Pinned[plan.Rules[0].Action], p) {
		t.Fatalf("pinned = %+v, want %+v", out.Pinned, p)
	}
}

func TestPatternsOrdersFavoritesNamesAndIDsWithArchivedFilter(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	var created []SavedPattern
	for _, entry := range []struct {
		name     string
		favorite bool
	}{
		{"zebra", false},
		{"Bravo", true},
		{"alpha", false},
		{"ALPHA", false},
		{"aardvark", true},
		{"archived", false},
	} {
		d := design()
		d.Name, d.Favorite = entry.name, entry.favorite
		p, err := s.CreatePattern(ctx, d, "owner")
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, p)
	}
	last := len(created) - 1
	created[last], err = s.ArchivePattern(ctx, created[last].ID, created[last].Revision, true, "owner")
	if err != nil {
		t.Fatal(err)
	}
	alpha := []SavedPattern{created[2], created[3]}
	sort.Slice(alpha, func(i, j int) bool { return alpha[i].ID < alpha[j].ID })
	want := []SavedPattern{created[4], created[1], alpha[0], alpha[1], created[0]}
	got, err := s.Patterns(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("active patterns = %+v, want %+v", got, want)
	}
	want = []SavedPattern{created[4], created[1], alpha[0], alpha[1], created[last], created[0]}
	got, err = s.Patterns(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("all patterns = %+v, want %+v", got, want)
	}
}

func TestPatternHistoryUnknownIDReturnsMissing(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.PatternHistory(context.Background(), "unknown"); !errors.Is(err, ErrMissing) {
		t.Fatal("unknown pattern history accepted", err)
	}
}
