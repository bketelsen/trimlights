package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"trimlights/internal/schedule"
)

var ErrScheduleConflict = errors.New("calendar changed on another device; reload before saving")
var ErrScheduleReference = errors.New("calendar references a missing or newly archived pattern revision")

type SavedSchedule struct {
	Revision int           `json:"revision"`
	Plan     schedule.Plan `json:"plan"`
	Actor    string        `json:"actor"`
	Updated  time.Time     `json:"updated"`
}
type ScheduleReference struct {
	RuleID  string          `json:"rule_id"`
	Name    string          `json:"name"`
	Enabled bool            `json:"enabled"`
	Action  schedule.Action `json:"action"`
}

func scheduleReferences(p schedule.Plan) []ScheduleReference {
	refs := []ScheduleReference{}
	for _, r := range p.Rules {
		if r.Action.Kind == "pattern" {
			refs = append(refs, ScheduleReference{r.ID, r.Name, r.Enabled, r.Action})
		}
	}
	if o := p.Override; o != nil && o.Action.Kind == "pattern" {
		refs = append(refs, ScheduleReference{o.ID, "Temporary override", true, o.Action})
	}
	return refs
}
func scanSchedule(row scanner) (SavedSchedule, error) {
	var s SavedSchedule
	var raw []byte
	var updated string
	err := row.Scan(&s.Revision, &raw, &s.Actor, &updated)
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(raw, &s.Plan); err != nil {
		return s, err
	}
	s.Updated, err = time.Parse(time.RFC3339Nano, updated)
	return s, err
}
func (s *Store) Schedule(ctx context.Context) (SavedSchedule, error) {
	return scanSchedule(s.db.QueryRowContext(ctx, "SELECT revision,body,actor,created FROM schedule_revisions ORDER BY revision DESC LIMIT 1"))
}
func (s *Store) ScheduleHistory(ctx context.Context) ([]SavedSchedule, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT revision,body,actor,created FROM schedule_revisions ORDER BY revision DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedSchedule{}
	for rows.Next() {
		v, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) SaveSchedule(ctx context.Context, expected int, p schedule.Plan, actor string) (SavedSchedule, error) {
	if err := p.Validate(); err != nil {
		return SavedSchedule{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SavedSchedule{}, err
	}
	defer func() { _ = tx.Rollback() }()
	current, err := scanSchedule(tx.QueryRowContext(ctx, "SELECT revision,body,actor,created FROM schedule_revisions ORDER BY revision DESC LIMIT 1"))
	if err != nil {
		return SavedSchedule{}, err
	}
	if current.Revision != expected {
		return SavedSchedule{}, ErrScheduleConflict
	}
	if err = checkScheduleReferences(ctx, tx, current.Plan, p); err != nil {
		return SavedSchedule{}, err
	}
	saved := SavedSchedule{Revision: expected + 1, Plan: p, Actor: actor, Updated: time.Now().UTC()}
	if err = insertSchedule(ctx, tx, saved); err != nil {
		return SavedSchedule{}, err
	}
	return saved, tx.Commit()
}
func insertSchedule(ctx context.Context, tx *sql.Tx, s SavedSchedule) error {
	raw, err := json.Marshal(s.Plan)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO schedule_revisions VALUES(?,?,?,?)", s.Revision, raw, s.Actor, s.Updated.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	for _, ref := range scheduleReferences(s.Plan) {
		if _, err = tx.ExecContext(ctx, "INSERT INTO schedule_references VALUES(?,?,?,?,?,?)", s.Revision, ref.RuleID, ref.Name, ref.Enabled, ref.Action.PatternID, ref.Action.Revision); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) PatternScheduleReferences(ctx context.Context, id string) ([]ScheduleReference, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT rule_id,name,enabled,pattern_id,pattern_revision FROM schedule_references WHERE schedule_revision=(SELECT MAX(revision) FROM schedule_revisions) AND pattern_id=? ORDER BY rule_id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScheduleReference{}
	for rows.Next() {
		var r ScheduleReference
		r.Action.Kind = "pattern"
		if err = rows.Scan(&r.RuleID, &r.Name, &r.Enabled, &r.Action.PatternID, &r.Action.Revision); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ValidateSchedule checks a draft against the same saved base and reference rules
// as SaveSchedule, without writing revisions or reference indexes.
func (s *Store) ValidateSchedule(ctx context.Context, expected int, p schedule.Plan) error {
	if err := p.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	current, err := scanSchedule(tx.QueryRowContext(ctx, "SELECT revision,body,actor,created FROM schedule_revisions ORDER BY revision DESC LIMIT 1"))
	if err != nil {
		return err
	}
	if current.Revision != expected {
		return ErrScheduleConflict
	}
	return checkScheduleReferences(ctx, tx, current.Plan, p)
}

func checkScheduleReferences(ctx context.Context, tx *sql.Tx, current, p schedule.Plan) error {
	existing := map[schedule.Action]bool{}
	for _, r := range scheduleReferences(current) {
		existing[r.Action] = true
	}
	refs := scheduleReferences(p)
	for _, r := range refs {
		var headArchived, revisionArchived bool
		err := tx.QueryRowContext(ctx, "SELECT h.archived,r.archived FROM pattern_revisions r JOIN pattern_heads h ON h.id=r.id WHERE r.id=? AND r.revision=?", r.Action.PatternID, r.Action.Revision).Scan(&headArchived, &revisionArchived)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrScheduleReference, r.Name)
		}
		if err != nil {
			return err
		}
		// Retain an existing pinned reference through archival; do not introduce a new
		// rule/override pointing at an archived pattern merely because another uses it.
		retained := false
		for _, old := range scheduleReferences(current) {
			if old.RuleID == r.RuleID && old.Action == r.Action {
				retained = true
				break
			}
		}
		if (headArchived || revisionArchived) && (!existing[r.Action] || !retained) {
			return fmt.Errorf("%w: %s", ErrScheduleReference, r.Name)
		}
	}
	return nil
}
