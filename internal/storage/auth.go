package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrMissing = errors.New("record missing or expired")

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
	defer func() { _ = tx.Rollback() }()
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
