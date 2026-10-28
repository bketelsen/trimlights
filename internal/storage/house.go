package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"trimlights/internal/house"
)

var ErrHouseConflict = errors.New("house mapping changed on another device; keep your draft or reload before saving")
var ErrPhotoLimit = errors.New("household photo limit reached (20)")
var ErrPhotoReference = errors.New("layout references a photo that does not exist in this household; restore the matching full backup first")

type Photo struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Width   int       `json:"width"`
	Height  int       `json:"height"`
	Created time.Time `json:"created"`
}
type SavedHouse struct {
	Revision int          `json:"revision"`
	Layout   house.Layout `json:"layout"`
	Actor    string       `json:"actor"`
	Updated  time.Time    `json:"updated"`
}

func (s *Store) AddPhoto(ctx context.Context, p house.PhotoImage, actor string) (Photo, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Photo{}, err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM photos").Scan(&count); err != nil {
		return Photo{}, err
	}
	if count >= 20 {
		return Photo{}, ErrPhotoLimit
	}
	result := Photo{ID: rand.Text(), Name: "House photo", Width: p.Width, Height: p.Height, Created: time.Now().UTC()}
	_, err = tx.ExecContext(ctx, "INSERT INTO photos VALUES(?,?,?,?,?,?,?,?)", result.ID, result.Name, p.Width, p.Height, result.Created.Format(time.RFC3339Nano), actor, p.JPEG, p.Thumbnail)
	if err != nil {
		return Photo{}, err
	}
	return result, tx.Commit()
}
func (s *Store) Photos(ctx context.Context) ([]Photo, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,name,width,height,created FROM photos ORDER BY created")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Photo{}
	for rows.Next() {
		var p Photo
		var created string
		if err = rows.Scan(&p.ID, &p.Name, &p.Width, &p.Height, &created); err != nil {
			return nil, err
		}
		p.Created, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) PhotoBytes(ctx context.Context, id string, thumbnail bool) ([]byte, error) {
	field := "image"
	if thumbnail {
		field = "thumbnail"
	}
	var raw []byte
	err := s.db.QueryRowContext(ctx, "SELECT "+field+" FROM photos WHERE id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrMissing
	}
	return raw, err
}
func scanHouse(row scanner) (SavedHouse, error) {
	var h SavedHouse
	var raw []byte
	var created string
	err := row.Scan(&h.Revision, &raw, &h.Actor, &created)
	if err != nil {
		return h, err
	}
	if err = json.Unmarshal(raw, &h.Layout); err != nil {
		return h, err
	}
	h.Updated, err = time.Parse(time.RFC3339Nano, created)
	return h, err
}
func (s *Store) House(ctx context.Context) (SavedHouse, error) {
	return scanHouse(s.db.QueryRowContext(ctx, "SELECT revision,body,actor,created FROM house_revisions ORDER BY revision DESC LIMIT 1"))
}
func (s *Store) HouseHistory(ctx context.Context) ([]SavedHouse, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT revision,body,actor,created FROM house_revisions ORDER BY revision DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedHouse{}
	for rows.Next() {
		h, err := scanHouse(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
func (s *Store) SaveHouse(ctx context.Context, expected int, l house.Layout, actor string) (SavedHouse, error) {
	if err := l.Validate(); err != nil {
		return SavedHouse{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SavedHouse{}, err
	}
	defer tx.Rollback()
	var current int
	if err = tx.QueryRowContext(ctx, "SELECT MAX(revision) FROM house_revisions").Scan(&current); err != nil {
		return SavedHouse{}, err
	}
	if current != expected {
		return SavedHouse{}, ErrHouseConflict
	}
	if err = checkHousePhotos(ctx, tx, l); err != nil {
		return SavedHouse{}, err
	}
	h := SavedHouse{expected + 1, l, actor, time.Now().UTC()}
	if err = insertHouse(ctx, tx, h); err != nil {
		return SavedHouse{}, err
	}
	return h, tx.Commit()
}
func insertHouse(ctx context.Context, tx *sql.Tx, h SavedHouse) error {
	raw, err := json.Marshal(h.Layout)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO house_revisions VALUES(?,?,?,?)", h.Revision, raw, h.Actor, h.Updated.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	for _, v := range h.Layout.Views {
		if _, err = tx.ExecContext(ctx, "INSERT INTO house_photo_refs VALUES(?,?)", h.Revision, v.PhotoID); err != nil {
			return err
		}
	}
	return nil
}

// ValidateHouse verifies an imported draft without creating any revision.
func (s *Store) ValidateHouse(ctx context.Context, l house.Layout) error {
	if err := l.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return checkHousePhotos(ctx, tx, l)
}
func checkHousePhotos(ctx context.Context, tx *sql.Tx, l house.Layout) error {
	for _, v := range l.Views {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM photos WHERE id=?)", v.PhotoID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrPhotoReference
		}
	}
	return nil
}
