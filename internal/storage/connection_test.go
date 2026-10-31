package storage

import (
	"context"
	"database/sql/driver"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// A connection discarded after driver.ErrBadConn (for example after a cancelled
// query) is replaced; the replacement must carry the same PRAGMAs.
func TestReplacedConnectionKeepsPragmas(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Mark the first connection and clear its settings, so only a new connection
	// configured on open can pass the checks below.
	if _, err = conn.ExecContext(ctx, "CREATE TEMP TABLE first_connection(x); PRAGMA foreign_keys=OFF; PRAGMA busy_timeout=0;"); err != nil {
		t.Fatal(err)
	}
	if err = conn.Raw(func(any) error { return driver.ErrBadConn }); err != driver.ErrBadConn {
		t.Fatal(err)
	}
	conn.Close()
	var marked int
	if err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_temp_schema WHERE name='first_connection'").Scan(&marked); err != nil || marked != 0 {
		t.Fatal("connection was not replaced", marked, err)
	}
	var foreignKeys, busyTimeout, synchronous int
	var journal string
	if err = s.db.QueryRowContext(ctx, "SELECT (SELECT foreign_keys FROM pragma_foreign_keys),(SELECT timeout FROM pragma_busy_timeout),(SELECT synchronous FROM pragma_synchronous),(SELECT journal_mode FROM pragma_journal_mode)").Scan(&foreignKeys, &busyTimeout, &synchronous, &journal); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 || busyTimeout != 5000 || synchronous != 2 || journal != "wal" {
		t.Fatal(foreignKeys, busyTimeout, synchronous, journal)
	}
	// Retention removes the oldest operation; its control audit must cascade.
	start := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	for i := 0; i <= 100; i++ {
		stamp := start.Add(time.Duration(i) * time.Second)
		op := Operation{ID: fmt.Sprintf("op-%03d", i), Actor: "owner", Started: stamp, Finished: stamp, Control: &ControlAudit{Outcome: "verified"}}
		if err = s.Record(ctx, Observation{Source: "fixture", LastAttempt: stamp}, op); err != nil {
			t.Fatal(err)
		}
	}
	var operations, audits, orphans int
	if err = s.db.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM operations),(SELECT COUNT(*) FROM control_audits),(SELECT COUNT(*) FROM control_audits WHERE id NOT IN (SELECT id FROM operations))").Scan(&operations, &audits, &orphans); err != nil {
		t.Fatal(err)
	}
	if operations != 100 || audits != 100 || orphans != 0 {
		t.Fatal(operations, audits, orphans)
	}
}

// Data directory names are paths, not DSN syntax: '?', '%' and '#' must not
// redirect the database outside the locked directory or merge two directories.
func TestOpenKeepsDatabaseInsideUnusualDataDirectory(t *testing.T) {
	parent := t.TempDir()
	names := []string{"data?one", "data?two", "data%3Fone", "data%", "data#one", "data#two"}
	ctx := context.Background()
	stores := map[string]*Store{}
	for _, name := range names {
		dir := filepath.Join(parent, name)
		s, err := Open(dir)
		if err != nil {
			t.Fatal(name, err)
		}
		defer s.Close()
		stores[name] = s
		var file string
		if err = s.db.QueryRowContext(ctx, "SELECT file FROM pragma_database_list WHERE name='main'").Scan(&file); err != nil {
			t.Fatal(name, err)
		}
		if want := filepath.Join(dir, "trimlights.db"); file != want {
			t.Fatalf("%s opened %q, want %q", name, file, want)
		}
		if err = s.Record(ctx, Observation{Source: name}, Operation{ID: name, Actor: "test"}); err != nil {
			t.Fatal(name, err)
		}
	}
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(parent, name, "trimlights.db")); err != nil {
			t.Fatal(name, err)
		}
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, entry := range entries {
		found = append(found, entry.Name())
	}
	want := append([]string(nil), names...)
	sort.Strings(want)
	if fmt.Sprint(found) != fmt.Sprint(want) {
		t.Fatal("unexpected files beside data directories:", found)
	}
	// Each directory holds its own database, so no write overwrote another.
	for name, s := range stores {
		if o, err := s.Observation(ctx); err != nil || o.Source != name {
			t.Fatalf("%s holds observation %q: %v", name, o.Source, err)
		}
	}
}
