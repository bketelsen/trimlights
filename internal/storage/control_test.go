package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"trimlights/internal/trimlight"
)

func TestControlMigrationIntentCompletionAndCrashRecovery(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DROP TABLE control_audits; PRAGMA user_version=5"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	backups, _ := filepath.Glob(filepath.Join(dir, "before-schema-5-*.db"))
	if len(backups) != 1 {
		t.Fatal(backups)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	state := Observation{Source: "controller", LastAttempt: now}
	op := Operation{ID: "control", Actor: "owner", Started: now, Finished: now, Control: &ControlAudit{Request: trimlight.ControlRequest{Action: "off"}, Outcome: "pending"}}
	if err = s.Record(ctx, state, op); err != nil {
		t.Fatal(err)
	}
	op.Control.Outcome = "verified"
	op.Control.WriteAttempted = true
	if err = s.CompleteControl(ctx, state, op); err != nil {
		t.Fatal(err)
	}
	state.Source = "must-roll-back"
	if err = s.CompleteControl(ctx, state, op); err == nil {
		t.Fatal("completed twice")
	}
	stored, _ := s.Observation(ctx)
	if stored.Source != "controller" {
		t.Fatal("state was not atomic")
	}
	op.ID = "interrupted"
	op.Control.Outcome = "pending"
	if err = s.Record(ctx, stored, op); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.RecoverControlIntents(ctx); err != nil {
		t.Fatal(err)
	}
	ops, err := s.Operations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 2 || ops[0].ID != "interrupted" || ops[0].Control.Outcome != "uncertain" || ops[0].Error == "" {
		t.Fatal(ops)
	}
	stored, _ = s.Observation(ctx)
	if stored.LastError == "" {
		t.Fatal("interrupted intent not surfaced")
	}
	if err = s.RecoverControlIntents(ctx); err != nil {
		t.Fatal(err)
	}
}
