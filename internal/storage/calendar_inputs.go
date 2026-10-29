package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"trimlights/internal/schedule"
)

// CalendarInputs is a consistent, read-only view for advisory preparation. These
// cached values never constitute permission for a future controller write.
type CalendarInputs struct {
	Revision    int
	Observation Observation
	Heads       []SavedPattern
	Pinned      map[schedule.Action]SavedPattern
}

func (s *Store) CalendarInputs(ctx context.Context, expected int, plan schedule.Plan) (CalendarInputs, error) {
	out := CalendarInputs{Heads: []SavedPattern{}, Pinned: map[schedule.Action]SavedPattern{}}
	if err := plan.Validate(); err != nil {
		return out, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	current, err := scanSchedule(tx.QueryRowContext(ctx, "SELECT revision,body,actor,created FROM schedule_revisions ORDER BY revision DESC LIMIT 1"))
	if err != nil {
		return out, err
	}
	if current.Revision != expected {
		return out, ErrScheduleConflict
	}
	out.Revision = current.Revision
	if err = checkScheduleReferences(ctx, tx, current.Plan, plan); err != nil {
		return out, err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, "SELECT body FROM observation WHERE id=1").Scan(&raw)
	if err == nil {
		err = json.Unmarshal(raw, &out.Observation)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+patternFields+" FROM pattern_revisions r JOIN pattern_heads h ON h.id=r.id AND h.revision=r.revision ORDER BY r.id")
	if err != nil {
		return out, err
	}
	for rows.Next() {
		p, e := scanPattern(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Heads = append(out.Heads, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, ref := range scheduleReferences(plan) {
		if _, found := out.Pinned[ref.Action]; found {
			continue
		}
		p, e := scanPattern(tx.QueryRowContext(ctx, "SELECT "+patternFields+" FROM pattern_revisions r WHERE r.id=? AND r.revision=?", ref.Action.PatternID, ref.Action.Revision))
		if e != nil {
			return out, e
		}
		out.Pinned[ref.Action] = p
	}
	return out, tx.Commit()
}
