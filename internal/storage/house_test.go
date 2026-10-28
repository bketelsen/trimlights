package storage

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"trimlights/internal/house"
)

func TestHousePhotoReferencesConcurrentSaveAndReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	image := house.PhotoImage{JPEG: []byte("sanitized JPEG fixture"), Thumbnail: []byte("thumbnail"), Width: 200, Height: 100}
	p, err := s.AddPhoto(ctx, image, "one")
	if err != nil {
		t.Fatal(err)
	}
	l := house.Default()
	l.Runs = []house.Run{{ID: "front", Name: "Front", Output: "one", Start: 1, Count: 10}}
	l.Views = []house.View{{PhotoID: p.ID, Turns: 1, Mappings: []house.Mapping{{RunID: "front", Points: []house.Point{{X: 0, Y: 0}, {X: 1, Y: 1}}, Adjustments: []house.Adjustment{}}}}}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, actor := range []string{"one", "two"} {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.SaveHouse(ctx, 1, l, actor); results <- err }()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrHouseConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	l.Views[0].PhotoID = "missing"
	if _, err = s.SaveHouse(ctx, 2, l, "one"); !errors.Is(err, ErrPhotoReference) {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h, err := s.House(ctx)
	if err != nil || h.Revision != 2 || h.Layout.Views[0].PhotoID != p.ID {
		t.Fatal(h, err)
	}
	revisions, err := s.HouseHistory(ctx)
	if err != nil || len(revisions) != 2 || len(revisions[1].Layout.Views) != 0 {
		t.Fatal(revisions, err)
	}
	photos, err := s.Photos(ctx)
	if err != nil || len(photos) != 1 || photos[0].Width != 200 {
		t.Fatal(photos, err)
	}
	raw, err := s.PhotoBytes(ctx, p.ID, false)
	if err != nil || !bytes.Equal(raw, image.JPEG) {
		t.Fatal(err)
	}
	if _, err = s.PhotoBytes(ctx, "missing", true); !errors.Is(err, ErrMissing) {
		t.Fatal(err)
	}
	for i := 1; i < 20; i++ {
		if _, err = s.AddPhoto(ctx, image, "one"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.AddPhoto(ctx, image, "one"); !errors.Is(err, ErrPhotoLimit) {
		t.Fatal(err)
	}
}
func TestSchemaFourMigrationPreservesCalendarAndSessions(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = s.SaveSession(ctx, Session{Hash: "retained", Subject: "one", Expires: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	plan, err := s.Schedule(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plan.Plan.Timezone = "UTC"
	if _, err = s.SaveSchedule(ctx, 1, plan.Plan, "one"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DROP TABLE house_photo_refs; DROP TABLE house_revisions; DROP TABLE photos; PRAGMA user_version=4"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n int
	if err = s.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE hash='retained'").Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	plan, err = s.Schedule(ctx)
	if err != nil || plan.Revision != 2 || plan.Plan.Timezone != "UTC" {
		t.Fatal(plan, err)
	}
	h, err := s.House(ctx)
	if err != nil || h.Revision != 1 {
		t.Fatal(h, err)
	}
	backups, _ := filepath.Glob(filepath.Join(dir, "before-schema-4-*.db"))
	if len(backups) != 1 {
		t.Fatal(backups)
	}
}
