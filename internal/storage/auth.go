package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrMissing = errors.New("record missing or expired")

type LoginFlow struct {
	StateHash, BrowserHash, Nonce, Verifier string
	Expires                                 time.Time
}

func (s *Store) SaveFlow(ctx context.Context, f LoginFlow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM login_flows WHERE expires<=?", time.Now().Unix()); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM login_flows").Scan(&count); err != nil {
		return err
	}
	if count >= 100 {
		return errors.New("too many pending logins")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO login_flows VALUES(?,?,?,?,?)", f.StateHash, f.BrowserHash, f.Nonce, f.Verifier, f.Expires.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// ConsumeFlow atomically deletes a matching flow, even when it has expired.
func (s *Store) ConsumeFlow(ctx context.Context, state, browser string, now time.Time) (LoginFlow, error) {
	var f LoginFlow
	var expires int64
	err := s.db.QueryRowContext(ctx, "DELETE FROM login_flows WHERE state_hash=? AND browser_hash=? RETURNING nonce,verifier,expires", state, browser).Scan(&f.Nonce, &f.Verifier, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrMissing
	}
	if err != nil {
		return f, err
	}
	f.Expires = time.Unix(expires, 0)
	if !f.Expires.After(now) {
		return f, ErrMissing
	}
	return f, nil
}

type Session struct {
	Hash    string    `json:"-"`
	Subject string    `json:"subject"`
	Name    string    `json:"name"`
	CSRF    string    `json:"csrf_token"`
	Expires time.Time `json:"expires"`
}

func (s *Store) SaveSession(ctx context.Context, v Session) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE expires<=?", time.Now().Unix()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO sessions VALUES(?,?,?,?,?)", v.Hash, v.Subject, v.Name, v.CSRF, v.Expires.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Session(ctx context.Context, hash string, now time.Time) (Session, error) {
	var v Session
	var expires int64
	err := s.db.QueryRowContext(ctx, "SELECT subject,name,csrf,expires FROM sessions WHERE hash=?", hash).Scan(&v.Subject, &v.Name, &v.CSRF, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrMissing
	}
	if err != nil {
		return v, err
	}
	v.Hash = hash
	v.Expires = time.Unix(expires, 0)
	if !v.Expires.After(now) {
		return v, ErrMissing
	}
	return v, nil
}
func (s *Store) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE hash=?", hash)
	return err
}
