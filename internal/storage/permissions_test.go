package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

// An existing 0755 data directory and umask 0022 must not leave the database,
// WAL or SHM files readable by other users (issue #2). Not parallel: the umask
// is process-wide.
func TestOpenKeepsDataFilesPrivate(t *testing.T) {
	defer syscall.Umask(syscall.Umask(0o022))
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "trimlights.db")
	check := func(s *Store, hash string) {
		t.Helper()
		// A write keeps the WAL and SHM files in place while the store is open.
		if err := s.SaveSession(context.Background(), Session{Hash: hash, Subject: "s", Name: "n", CSRF: "c", Expires: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{db, db + "-wal", db + "-shm"} {
			if got := mode(t, path); got != 0o600 {
				t.Fatalf("%s has mode %04o, want 0600", path, got)
			}
		}
		if got := mode(t, dir); got != 0o700 {
			t.Fatalf("data directory has mode %04o, want 0700", got)
		}
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	check(s, "first")
	if notes := s.PermissionNotes(); len(notes) != 1 || !strings.Contains(notes[0], "from mode 0755 to 0700") {
		t.Fatal(notes)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}

	// Files an earlier version left readable are tightened on the next open.
	for _, path := range []string{dir, db} {
		if err = os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.Chmod(db, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	check(s, "second")
	if notes := s.PermissionNotes(); len(notes) != 2 || !strings.Contains(notes[1], "trimlights.db changed from mode 0644 to 0600") {
		t.Fatal(notes)
	}
}

// The migration backup is created 0600 before VACUUM INTO writes it, so it is
// never readable by other users, even briefly.
func TestMigrationBackupIsPrivate(t *testing.T) {
	defer syscall.Umask(syscall.Umask(0o022))
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("PRAGMA user_version=6"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	matches, err := filepath.Glob(filepath.Join(dir, "before-schema-6-*"))
	if err != nil || len(matches) != 1 {
		t.Fatal(matches, err)
	}
	if got := mode(t, matches[0]); got != 0o600 {
		t.Fatalf("migration backup has mode %04o", got)
	}
}
