package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"trimlights/internal/pattern"
	"trimlights/internal/trimlight"
)

func design() pattern.Design {
	return pattern.Design{Schema: 1, Name: "Warmth", Effect: "static", Brightness: 60, Palette: []pattern.Run{{Color: "#ffb05a", Count: 3}}}
}
func TestPatternConcurrentEditsAndRetainedHistory(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	ctx := context.Background()
	first, err := s.CreatePattern(ctx, design(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, name := range []string{"One", "Two"} {
		wg.Go(func() {
			d := design()
			d.Name = name
			_, err := s.UpdatePattern(ctx, first.ID, 1, d, name)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal(successes, conflicts)
	}
	history, err := s.PatternHistory(ctx, first.ID)
	if err != nil || len(history) != 2 || history[1].Design.Name != "Warmth" || history[1].Actor != "owner" {
		t.Fatal(history, err)
	}
	archived, err := s.ArchivePattern(ctx, first.ID, 2, true, "wife")
	if err != nil || archived.Revision != 3 || !archived.Archived {
		t.Fatal(archived, err)
	}
	values, err := s.Patterns(ctx, false)
	if err != nil || len(values) != 0 {
		t.Fatal(values, err)
	}
	if _, err = s.UpdatePattern(ctx, first.ID, 3, design(), "owner"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err = s.ArchivePattern(ctx, first.ID, 3, false, "owner"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	values, err = s.Patterns(ctx, false)
	if err != nil || len(values) != 1 || values[0].Revision != 4 {
		t.Fatal(values, err)
	}
	history, err = s.PatternHistory(ctx, first.ID)
	if err != nil || len(history) != 4 || !history[1].Archived {
		t.Fatal(history, err)
	}
}
func TestControllerImportIsIdempotentAndPreservesEdits(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	raw, err := os.ReadFile("../trimlight/testdata/snapshot.bin")
	if err != nil {
		t.Fatal(err)
	}
	snap, err := trimlight.ParseSnapshot(raw, 3306)
	if err != nil {
		t.Fatal(err)
	}
	snap.QueriedAt = time.Now().UTC()
	if err = s.Record(ctx, Observation{Snapshot: snap, Source: "fixture"}, Operation{ID: "import-fixture", Started: snap.QueriedAt, Finished: snap.QueriedAt}); err != nil {
		t.Fatal(err)
	}
	result, err := s.ImportControllerPatterns(ctx, "owner")
	if err != nil || len(result.Imported) != 18 {
		t.Fatal(result, err)
	}
	first := result.Imported[0]
	d := first.Design
	d.Name = "Renamed"
	d.Favorite = true
	if _, err = s.UpdatePattern(ctx, first.ID, 1, d, "wife"); err != nil {
		t.Fatal(err)
	}
	result, err = s.ImportControllerPatterns(ctx, "owner")
	if err != nil || result.Skipped != 18 || len(result.Imported) != 0 {
		t.Fatal(result, err)
	}
	p, err := s.Pattern(ctx, first.ID)
	if err != nil || p.Design.Name != "Renamed" || p.Design.Native.ParametersHex != snap.Patterns[0].ParametersHex {
		t.Fatal(p, err)
	}
	if _, err = s.UpdatePattern(ctx, first.ID, 2, design(), "owner"); !errors.Is(err, ErrNativeEdit) {
		t.Fatal(err)
	}
	// Duplicate an exported design without reusing its application ID.
	duplicate, err := s.CreatePattern(ctx, p.Design, "owner")
	if err != nil || duplicate.ID == p.ID {
		t.Fatal(duplicate, err)
	}
}
func TestInvalidControllerImportRollsBackWholeBatch(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	stamp := time.Now().UTC()
	snap := &trimlight.Snapshot{QueriedAt: stamp, Patterns: []trimlight.Pattern{{ID: 1, Name: "First", ParametersHex: strings.Repeat("00", 156)}, {ID: 2, Name: "Second", ParametersHex: "bad"}}}
	if err = s.Record(ctx, Observation{Snapshot: snap}, Operation{ID: "fixture", Started: stamp, Finished: stamp}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ImportControllerPatterns(ctx, "owner"); err == nil {
		t.Fatal("invalid capture imported")
	}
	values, err := s.Patterns(ctx, true)
	if err != nil || len(values) != 0 {
		t.Fatal(values, err)
	}
}

func TestControllerImportReplacesInvalidSlotNames(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	stamp := time.Now().UTC()
	params := strings.Repeat("00", 156)
	raw := []string{"", "bad\x01name", "\ufeffMark", "  ", " Porch ", "Good"}
	snap := &trimlight.Snapshot{QueriedAt: stamp}
	for i, name := range raw {
		snap.Patterns = append(snap.Patterns, trimlight.Pattern{ID: byte(i + 1), Name: name, ParametersHex: params})
	}
	if err = s.Record(ctx, Observation{Snapshot: snap}, Operation{ID: "fixture", Started: stamp, Finished: stamp}); err != nil {
		t.Fatal(err)
	}
	result, err := s.ImportControllerPatterns(ctx, "owner")
	if err != nil || len(result.Imported) != len(raw) {
		t.Fatal(result, err)
	}
	want := []string{"Controller pattern 1", "Controller pattern 2", "Controller pattern 3", "Controller pattern 4", "Porch", "Good"}
	for i, p := range result.Imported {
		if p.Design.Name != want[i] || p.Design.Native.Name != raw[i] || p.Design.Native.ID != byte(i+1) {
			t.Fatalf("slot %d: %+v %+v", i+1, p.Design, p.Design.Native)
		}
	}
}

func TestCreateNativeRequiresCachedSlotMatch(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	stamp := time.Now().UTC()
	params := strings.Repeat("ab", 156)
	native := pattern.Design{Schema: 1, Name: "Copy", Effect: "native", Brightness: 100, Palette: []pattern.Run{}, Native: &pattern.Native{ID: 3, Name: "Slot", ParametersHex: params, SnapshotAt: stamp}}
	if _, err = s.CreatePattern(ctx, native, "owner"); !errors.Is(err, ErrNativeSource) {
		t.Fatal("native design created without a cached snapshot", err)
	}
	snap := &trimlight.Snapshot{QueriedAt: stamp, Patterns: []trimlight.Pattern{{ID: 3, Name: "Slot", ParametersHex: params}}}
	if err = s.Record(ctx, Observation{Snapshot: snap}, Operation{ID: "fixture", Started: stamp, Finished: stamp}); err != nil {
		t.Fatal(err)
	}
	for _, forge := range []func(*pattern.Native){
		func(n *pattern.Native) { n.ID = 4 },
		func(n *pattern.Native) { n.ParametersHex = strings.Repeat("00", 156) },
		func(n *pattern.Native) { n.Name = "Forged\nname" },
	} {
		d := native
		copied := *native.Native
		forge(&copied)
		d.Native = &copied
		if _, err = s.CreatePattern(ctx, d, "owner"); !errors.Is(err, ErrNativeSource) {
			t.Fatal("forged native design created", copied, err)
		}
	}
	d := native
	copied := *native.Native
	copied.ParametersHex = strings.ToUpper(params)
	d.Native = &copied
	if _, err = s.CreatePattern(ctx, d, "owner"); err != nil {
		t.Fatal("matching native duplicate rejected", err)
	}
	values, err := s.Patterns(ctx, true)
	if err != nil || len(values) != 1 {
		t.Fatal(values, err)
	}
}

func TestSchemaTwoUpgradeRetainsSessionsAndCreatesPatterns(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	session := Session{Hash: "retained", Subject: "owner", Name: "Owner", CSRF: "csrf", Expires: time.Now().Add(time.Hour)}
	if err = s.SaveSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DROP TABLE schedule_references; DROP TABLE schedule_revisions; DROP TABLE pattern_imports; DROP TABLE pattern_revisions; DROP TABLE pattern_heads; PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.Session(ctx, "retained", time.Now()); err != nil || got.Subject != "owner" {
		t.Fatal(got, err)
	}
	if _, err = s.CreatePattern(ctx, design(), "owner"); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(dir, "before-schema-2-*.db"))
	if err != nil || len(backups) != 1 {
		t.Fatal(backups, err)
	}
}
