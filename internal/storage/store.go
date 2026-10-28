// Package storage owns the application's SQLite connection and schema.
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
	"trimlights/internal/house"
	"trimlights/internal/schedule"
	"trimlights/internal/trimlight"
)

type Store struct {
	db   *sql.DB
	lock *os.File
	dir  string
}

type Observation struct {
	Snapshot    *trimlight.Snapshot `json:"snapshot,omitempty"`
	LastAttempt time.Time           `json:"last_attempt,omitempty"`
	LastError   string              `json:"last_error,omitempty"`
	Source      string              `json:"source"`
}

// Open requires a local filesystem. The flock is held for the store's lifetime,
// preventing two processes using this data directory from owning the controller.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "service.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return nil, fmt.Errorf("data directory already owned: %w", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "trimlights.db"))
	if err != nil {
		lock.Close()
		return nil, err
	}
	s := &Store{db: db, lock: lock, dir: dir}
	db.SetMaxOpenConns(1)
	if err = s.init(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) init() error {
	if _, err := s.db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA synchronous=FULL;`); err != nil {
		return err
	}
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 5 {
		return fmt.Errorf("database schema %d is newer than this binary supports", version)
	}
	if version == 5 {
		return nil
	}
	if version > 0 {
		backup := filepath.Join(s.dir, fmt.Sprintf("before-schema-%d-%d.db", version, time.Now().UnixNano()))
		if _, err := s.db.Exec("VACUUM INTO ?", backup); err != nil {
			return fmt.Errorf("migration backup: %w", err)
		}
		if err := os.Chmod(backup, 0600); err != nil {
			return err
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if version == 0 {
		_, err = tx.Exec(`CREATE TABLE observation(id INTEGER PRIMARY KEY CHECK(id=1), body BLOB NOT NULL);
 CREATE TABLE operations(id TEXT PRIMARY KEY, started TEXT NOT NULL, finished TEXT NOT NULL, actor TEXT NOT NULL, error TEXT NOT NULL);
 PRAGMA user_version=1;`)
		if err != nil {
			return err
		}
	}
	if version < 2 {
		_, err = tx.Exec(`CREATE TABLE login_flows(state_hash TEXT PRIMARY KEY,browser_hash TEXT NOT NULL,nonce TEXT NOT NULL,verifier TEXT NOT NULL,expires INTEGER NOT NULL);
 CREATE TABLE sessions(hash TEXT PRIMARY KEY,subject TEXT NOT NULL,name TEXT NOT NULL,csrf TEXT NOT NULL,expires INTEGER NOT NULL);
 PRAGMA user_version=2;`)
		if err != nil {
			return err
		}
	}
	if version < 3 {
		_, err = tx.Exec(`CREATE TABLE pattern_heads(id TEXT PRIMARY KEY,revision INTEGER NOT NULL,archived INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE pattern_revisions(id TEXT NOT NULL REFERENCES pattern_heads(id),revision INTEGER NOT NULL,body BLOB NOT NULL,actor TEXT NOT NULL,created TEXT NOT NULL,archived INTEGER NOT NULL,PRIMARY KEY(id,revision));
 CREATE TABLE pattern_imports(slot INTEGER PRIMARY KEY,pattern_id TEXT NOT NULL REFERENCES pattern_heads(id));
 PRAGMA user_version=3;`)
		if err != nil {
			return err
		}
	}
	if version < 4 {
		_, err = tx.Exec(`CREATE TABLE schedule_revisions(revision INTEGER PRIMARY KEY,body BLOB NOT NULL,actor TEXT NOT NULL,created TEXT NOT NULL);
 CREATE TABLE schedule_references(schedule_revision INTEGER NOT NULL REFERENCES schedule_revisions(revision),rule_id TEXT NOT NULL,name TEXT NOT NULL,enabled INTEGER NOT NULL,pattern_id TEXT NOT NULL,pattern_revision INTEGER NOT NULL,PRIMARY KEY(schedule_revision,rule_id),FOREIGN KEY(pattern_id,pattern_revision) REFERENCES pattern_revisions(id,revision));
 CREATE INDEX schedule_pattern_refs ON schedule_references(pattern_id,schedule_revision);
 PRAGMA user_version=4;`)
		if err != nil {
			return err
		}
		if err = insertSchedule(context.Background(), tx, SavedSchedule{Revision: 1, Plan: schedule.Default(), Actor: "system", Updated: time.Now().UTC()}); err != nil {
			return err
		}
	}
	if version < 5 {
		_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS photos(id TEXT PRIMARY KEY,name TEXT NOT NULL,width INTEGER NOT NULL,height INTEGER NOT NULL,created TEXT NOT NULL,actor TEXT NOT NULL,image BLOB NOT NULL,thumbnail BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS house_revisions(revision INTEGER PRIMARY KEY,body BLOB NOT NULL,actor TEXT NOT NULL,created TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS house_photo_refs(revision INTEGER NOT NULL REFERENCES house_revisions(revision),photo_id TEXT NOT NULL REFERENCES photos(id),PRIMARY KEY(revision,photo_id));
 PRAGMA user_version=5;`)
		if err != nil {
			return err
		}
		var count int
		if err = tx.QueryRow("SELECT COUNT(*) FROM house_revisions").Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if err = insertHouse(context.Background(), tx, SavedHouse{Revision: 1, Layout: house.Default(), Actor: "system", Updated: time.Now().UTC()}); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func (s *Store) Close() error { return errors.Join(s.db.Close(), s.lock.Close()) }
func (s *Store) Observation(ctx context.Context) (Observation, error) {
	var b []byte
	err := s.db.QueryRowContext(ctx, "SELECT body FROM observation WHERE id=1").Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return Observation{Source: "none"}, nil
	}
	if err != nil {
		return Observation{}, err
	}
	var o Observation
	err = json.Unmarshal(b, &o)
	return o, err
}

type Operation struct {
	ID       string    `json:"id"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Actor    string    `json:"actor"`
	Error    string    `json:"error,omitempty"`
}

// Record atomically publishes state and its audit result. Failed reads preserve
// the last successful snapshot, whose timestamp remains explicitly stale.
func (s *Store) Record(ctx context.Context, o Observation, op Operation) error {
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO observation VALUES(1,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", b); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO operations VALUES(?,?,?,?,?)", op.ID, op.Started.UTC().Format("2006-01-02T15:04:05.000000000Z"), op.Finished.UTC().Format("2006-01-02T15:04:05.000000000Z"), op.Actor, op.Error); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM operations WHERE id NOT IN (SELECT id FROM operations ORDER BY finished DESC LIMIT 100)"); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Operations(ctx context.Context) ([]Operation, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,started,finished,actor,error FROM operations ORDER BY finished DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Operation{}
	for rows.Next() {
		var op Operation
		var start, end string
		if err = rows.Scan(&op.ID, &start, &end, &op.Actor, &op.Error); err != nil {
			return nil, err
		}
		op.Started, err = time.Parse(time.RFC3339Nano, start)
		if err != nil {
			return nil, err
		}
		op.Finished, err = time.Parse(time.RFC3339Nano, end)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, rows.Err()
}
