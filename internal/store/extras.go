package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Extra is a next stop after the meeting's main idea («А потом?»).
type Extra struct {
	ID         int64
	RoundID    int64
	IdeaID     int64
	Pos        int
	ProposedBy int64
	State      string // proposed | accepted | declined
	CreatedAt  time.Time
}

const extraCols = `id, round_id, idea_id, pos, proposed_by, state, created_at`

func scanExtra(sc interface{ Scan(...any) error }) (Extra, error) {
	var e Extra
	var c int64
	err := sc.Scan(&e.ID, &e.RoundID, &e.IdeaID, &e.Pos, &e.ProposedBy, &e.State, &c)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	e.CreatedAt = time.Unix(c, 0)
	return e, err
}

func (s *Store) AddExtra(ctx context.Context, e Extra, now time.Time) (Extra, error) {
	var pos int
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(pos),0)+1 FROM round_extras WHERE round_id=? AND state!='declined'`, e.RoundID).Scan(&pos)
	r, err := s.DB.ExecContext(ctx, `INSERT INTO round_extras(round_id, idea_id, pos, proposed_by, state, created_at) VALUES (?,?,?,?,'proposed',?)`,
		e.RoundID, e.IdeaID, pos, e.ProposedBy, now.Unix())
	if err != nil {
		return e, err
	}
	e.ID, _ = r.LastInsertId()
	e.Pos, e.State, e.CreatedAt = pos, "proposed", now
	return e, nil
}

// Extras lists the live (not declined) stops in order.
func (s *Store) Extras(ctx context.Context, roundID int64) ([]Extra, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+extraCols+` FROM round_extras WHERE round_id=? AND state!='declined' ORDER BY pos, id`, roundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Extra{}
	for rows.Next() {
		e, err := scanExtra(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) Extra(ctx context.Context, id int64) (Extra, error) {
	return scanExtra(s.DB.QueryRowContext(ctx, `SELECT `+extraCols+` FROM round_extras WHERE id=?`, id))
}

// SetExtraState moves a proposed stop to accepted/declined; false when it was not proposed anymore.
func (s *Store) SetExtraState(ctx context.Context, id int64, state string) (bool, error) {
	r, err := s.DB.ExecContext(ctx, `UPDATE round_extras SET state=? WHERE id=? AND state='proposed'`, state, id)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n == 1, nil
}

func (s *Store) DeleteExtra(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM round_extras WHERE id=?`, id)
	return err
}
