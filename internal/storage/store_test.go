package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
	"trimlights/internal/trimlight"
)

func TestStoreLockReopenAndAtomicAudit(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := Open(dir); err == nil {
		other.Close()
		t.Fatal("second owner accepted")
	}
	ctx := context.Background()
	stamp := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	original := Observation{Snapshot: &trimlight.Snapshot{QueriedAt: stamp, Patterns: []trimlight.Pattern{{ID: 1, Name: "Original"}}}, Source: "controller", LastAttempt: stamp}
	op := Operation{ID: "one", Actor: "test", Started: stamp, Finished: stamp}
	if err = s.Record(ctx, original, op); err != nil {
		t.Fatal(err)
	}
	changed := original
	changed.Source = "must-roll-back"
	if err = s.Record(ctx, changed, op); err == nil {
		t.Fatal("duplicate operation accepted")
	}
	got, err := s.Observation(ctx)
	if err != nil || got.Source != "controller" {
		t.Fatal(got, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err = s.Observation(ctx)
	if err != nil || got.Snapshot.Patterns[0].Name != "Original" {
		t.Fatal(got, err)
	}
	for i := 0; i < 105; i++ {
		op.ID = fmt.Sprint(i)
		op.Finished = stamp.Add(time.Duration(i+1) * time.Second)
		if err = s.Record(ctx, original, op); err != nil {
			t.Fatal(err)
		}
	}
	ops, err := s.Operations(ctx)
	if err != nil || len(ops) != 100 || ops[0].ID != "104" {
		t.Fatal(len(ops), err)
	}
}
func TestFutureSchemaRefused(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("PRAGMA user_version=100"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if s, err = Open(dir); err == nil {
		s.Close()
		t.Fatal("future schema accepted")
	}
}

func TestMigrationBacksUpExistingDatabase(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct the released version-1 schema to exercise a real upgrade.
	if _, err = s.db.Exec("DROP TABLE schedule_references; DROP TABLE schedule_revisions; DROP TABLE pattern_imports; DROP TABLE pattern_revisions; DROP TABLE pattern_heads; DROP TABLE login_flows; DROP TABLE sessions; PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	matches, err := filepath.Glob(filepath.Join(dir, "before-schema-1-*.db"))
	if err != nil || len(matches) != 1 {
		t.Fatal(matches, err)
	}
	backup, err := sql.Open("sqlite", matches[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var v int
	if err = backup.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != 1 {
		t.Fatal(v, err)
	}
	session := Session{Hash: "hash", Subject: "owner", Name: "Owner", CSRF: "csrf", Expires: time.Now().Add(time.Hour)}
	if err = s.SaveSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	restored, err := s.Session(context.Background(), "hash", time.Now())
	if err != nil || restored.Subject != "owner" {
		t.Fatal(restored, err)
	}
}
