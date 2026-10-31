package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"trimlights/internal/pattern"
	"trimlights/internal/trimlight"
)

var ErrNativeEdit = errors.New("native pattern parameters cannot be changed; create a separate custom design")

var ErrConflict = errors.New("pattern changed or was archived; reload before saving")

// ErrNativeSource rejects a created native design that does not copy a slot of
// the latest cached controller snapshot. Native designs come from controller
// import, or duplicate/re-import one that still matches its slot.
var ErrNativeSource = errors.New("a native pattern can be created only as a copy of a matching slot in the latest controller snapshot; use import from the controller")

type SavedPattern struct {
	ID       string         `json:"id"`
	Revision int            `json:"revision"`
	Design   pattern.Design `json:"design"`
	Actor    string         `json:"actor"`
	Updated  time.Time      `json:"updated"`
	Archived bool           `json:"archived"`
}
type scanner interface{ Scan(...any) error }

func scanPattern(row scanner) (SavedPattern, error) {
	var p SavedPattern
	var raw []byte
	var created string
	err := row.Scan(&p.ID, &p.Revision, &raw, &p.Actor, &created, &p.Archived)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrMissing
	}
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(raw, &p.Design); err != nil {
		return p, err
	}
	p.Updated, err = time.Parse(time.RFC3339Nano, created)
	return p, err
}

const patternFields = "r.id,r.revision,r.body,r.actor,r.created,r.archived"

func (s *Store) Pattern(ctx context.Context, id string) (SavedPattern, error) {
	return scanPattern(s.db.QueryRowContext(ctx, "SELECT "+patternFields+" FROM pattern_revisions r JOIN pattern_heads h ON h.id=r.id AND h.revision=r.revision WHERE h.id=?", id))
}
func (s *Store) Patterns(ctx context.Context, includeArchived bool) ([]SavedPattern, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+patternFields+" FROM pattern_revisions r JOIN pattern_heads h ON h.id=r.id AND h.revision=r.revision WHERE h.archived=0 OR ?", includeArchived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedPattern{}
	for rows.Next() {
		p, err := scanPattern(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Design.Favorite != out[j].Design.Favorite {
			return out[i].Design.Favorite
		}
		a, b := strings.ToLower(out[i].Design.Name), strings.ToLower(out[j].Design.Name)
		if a == b {
			return out[i].ID < out[j].ID
		}
		return a < b
	})
	return out, rows.Err()
}
func (s *Store) PatternHistory(ctx context.Context, id string) ([]SavedPattern, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+patternFields+" FROM pattern_revisions r WHERE id=? ORDER BY revision DESC", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedPattern{}
	for rows.Next() {
		p, err := scanPattern(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrMissing
	}
	return out, nil
}
func writeRevision(ctx context.Context, tx *sql.Tx, p SavedPattern) error {
	b, err := json.Marshal(p.Design)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO pattern_revisions VALUES(?,?,?,?,?,?)", p.ID, p.Revision, b, p.Actor, p.Updated.UTC().Format("2006-01-02T15:04:05.000000000Z"), p.Archived)
	return err
}
func createPattern(ctx context.Context, tx *sql.Tx, d pattern.Design, actor string) (SavedPattern, error) {
	p := SavedPattern{ID: rand.Text(), Revision: 1, Design: d, Actor: actor, Updated: time.Now().UTC()}
	if _, err := tx.ExecContext(ctx, "INSERT INTO pattern_heads VALUES(?,1,0)", p.ID); err != nil {
		return p, err
	}
	return p, writeRevision(ctx, tx, p)
}
func (s *Store) CreatePattern(ctx context.Context, d pattern.Design, actor string) (SavedPattern, error) {
	if err := d.Validate(); err != nil {
		return SavedPattern{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SavedPattern{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if d.Native != nil {
		if err = matchCachedSlot(ctx, tx, *d.Native); err != nil {
			return SavedPattern{}, err
		}
	}
	p, err := createPattern(ctx, tx, d, actor)
	if err != nil {
		return p, err
	}
	return p, tx.Commit()
}

// matchCachedSlot accepts a native copy only when slot ID, raw slot name and all
// parameter bytes equal a slot of the cached snapshot, so a crafted file or
// request cannot add forged native entries.
func matchCachedSlot(ctx context.Context, tx *sql.Tx, n pattern.Native) error {
	snap, err := cachedSnapshot(ctx, tx)
	if errors.Is(err, ErrMissing) {
		return ErrNativeSource
	}
	if err != nil {
		return err
	}
	for _, slot := range snap.Patterns {
		if slot.ID == n.ID && slot.Name == n.Name && strings.EqualFold(slot.ParametersHex, n.ParametersHex) {
			return nil
		}
	}
	return ErrNativeSource
}
func cachedSnapshot(ctx context.Context, tx *sql.Tx) (*trimlight.Snapshot, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT body FROM observation WHERE id=1").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMissing
	}
	if err != nil {
		return nil, err
	}
	var observation Observation
	if err = json.Unmarshal(raw, &observation); err != nil {
		return nil, err
	}
	if observation.Snapshot == nil || observation.Snapshot.QueriedAt.IsZero() {
		return nil, ErrMissing
	}
	return observation.Snapshot, nil
}
func (s *Store) UpdatePattern(ctx context.Context, id string, expected int, d pattern.Design, actor string) (SavedPattern, error) {
	if err := d.Validate(); err != nil {
		return SavedPattern{}, err
	}
	return s.mutatePattern(ctx, id, expected, actor, func(p *SavedPattern) error {
		if p.Archived {
			return ErrConflict
		}
		if p.Design.Native != nil && (d.Effect != "native" || !reflect.DeepEqual(p.Design.Native, d.Native)) {
			return ErrNativeEdit
		}
		if p.Design.Native == nil && d.Native != nil {
			return ErrNativeEdit
		}
		p.Design = d
		return nil
	})
}
func (s *Store) ArchivePattern(ctx context.Context, id string, expected int, archived bool, actor string) (SavedPattern, error) {
	return s.mutatePattern(ctx, id, expected, actor, func(p *SavedPattern) error { p.Archived = archived; return nil })
}
func (s *Store) mutatePattern(ctx context.Context, id string, expected int, actor string, mutate func(*SavedPattern) error) (SavedPattern, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SavedPattern{}, err
	}
	defer func() { _ = tx.Rollback() }()
	p, err := scanPattern(tx.QueryRowContext(ctx, "SELECT "+patternFields+" FROM pattern_revisions r JOIN pattern_heads h ON h.id=r.id AND h.revision=r.revision WHERE h.id=?", id))
	if err != nil {
		return p, err
	}
	if p.Revision != expected {
		return p, ErrConflict
	}
	if err = mutate(&p); err != nil {
		return p, err
	}
	p.Revision++
	p.Actor = actor
	p.Updated = time.Now().UTC()
	result, err := tx.ExecContext(ctx, "UPDATE pattern_heads SET revision=?,archived=? WHERE id=? AND revision=?", p.Revision, p.Archived, id, expected)
	if err != nil {
		return p, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return p, err
	}
	if affected != 1 {
		return p, ErrConflict
	}
	if err = writeRevision(ctx, tx, p); err != nil {
		return p, err
	}
	return p, tx.Commit()
}

type ImportResult struct {
	Imported   []SavedPattern `json:"imported"`
	Skipped    int            `json:"skipped"`
	SnapshotAt time.Time      `json:"snapshot_at"`
}

func (s *Store) ImportControllerPatterns(ctx context.Context, actor string) (ImportResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ImportResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	snap, err := cachedSnapshot(ctx, tx)
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{Imported: []SavedPattern{}, SnapshotAt: snap.QueriedAt}
	for _, native := range snap.Patterns {
		var existing string
		err = tx.QueryRowContext(ctx, "SELECT pattern_id FROM pattern_imports WHERE slot=?", native.ID).Scan(&existing)
		if err == nil {
			result.Skipped++
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
		// A blank or hidden-character slot name must not block the batch. The
		// display name falls back to the slot; Native.Name keeps the raw bytes.
		name := strings.TrimSpace(native.Name)
		if !pattern.ValidName(name) {
			name = fmt.Sprintf("Controller pattern %d", native.ID)
		}
		d := pattern.Design{Schema: 1, Name: name, Effect: "native", Brightness: 100, Palette: []pattern.Run{}, Native: &pattern.Native{ID: native.ID, Name: native.Name, ParametersHex: native.ParametersHex, SnapshotAt: result.SnapshotAt}}
		if err = d.Validate(); err != nil {
			return ImportResult{}, fmt.Errorf("controller slot %d: %w", native.ID, err)
		}
		p, err := createPattern(ctx, tx, d, actor)
		if err != nil {
			return ImportResult{}, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO pattern_imports VALUES(?,?)", native.ID, p.ID); err != nil {
			return ImportResult{}, err
		}
		result.Imported = append(result.Imported, p)
	}
	return result, tx.Commit()
}
