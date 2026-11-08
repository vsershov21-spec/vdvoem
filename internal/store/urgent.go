package store

import (
	"context"
	"database/sql"
	"time"
)

type Urgent struct {
	IdeaID, SpaceID, By int64
	Note                string
	CreatedAt, NextAt   time.Time
	Nudges              int
	DoneAt              *time.Time
	Answer              string
}

const urgentCols = `idea_id, space_id, by_member, note, created_at, next_at, nudges, done_at, answer`

func scanUrgent(r interface{ Scan(...any) error }) (Urgent, error) {
	var u Urgent
	var c, n int64
	var d sql.NullInt64
	err := r.Scan(&u.IdeaID, &u.SpaceID, &u.By, &u.Note, &c, &n, &u.Nudges, &d, &u.Answer)
	u.CreatedAt, u.NextAt, u.DoneAt = time.Unix(c, 0), time.Unix(n, 0), tp(d)
	return u, err
}

// SetUrgent opens (or reopens) the request on an idea
func (s *Store) SetUrgent(ctx context.Context, u Urgent) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO urgent(`+urgentCols+`) VALUES (?,?,?,?,?,?,0,NULL,'')
		ON CONFLICT(idea_id) DO UPDATE SET by_member=excluded.by_member, note=excluded.note, created_at=excluded.created_at,
		next_at=excluded.next_at, nudges=0, done_at=NULL, answer=''`,
		u.IdeaID, u.SpaceID, u.By, u.Note, u.CreatedAt.Unix(), u.NextAt.Unix())
	return err
}

// Urgent: the request on an idea, open or not (ErrNotFound — none)
func (s *Store) Urgent(ctx context.Context, spaceID, ideaID int64) (Urgent, error) {
	u, err := scanUrgent(s.DB.QueryRowContext(ctx, `SELECT `+urgentCols+` FROM urgent WHERE idea_id=? AND space_id=?`, ideaID, spaceID))
	if err == sql.ErrNoRows {
		return u, ErrNotFound
	}
	return u, err
}

// OpenUrgent: requests in a space still waiting for an answer
func (s *Store) OpenUrgent(ctx context.Context, spaceID int64) ([]Urgent, error) {
	return s.urgents(ctx, `WHERE space_id=? AND done_at IS NULL ORDER BY created_at`, spaceID)
}

// DueUrgent: open requests whose reminder is due
func (s *Store) DueUrgent(ctx context.Context, now time.Time) ([]Urgent, error) {
	return s.urgents(ctx, `WHERE done_at IS NULL AND next_at<=? ORDER BY next_at`, now.Unix())
}

func (s *Store) urgents(ctx context.Context, where string, args ...any) ([]Urgent, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+urgentCols+` FROM urgent `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Urgent
	for rows.Next() {
		u, err := scanUrgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// NudgeUrgent: a reminder went out; the next one at next
func (s *Store) NudgeUrgent(ctx context.Context, ideaID int64, next time.Time, counted bool) error {
	q := `UPDATE urgent SET next_at=? WHERE idea_id=? AND done_at IS NULL`
	if counted {
		q = `UPDATE urgent SET next_at=?, nudges=nudges+1 WHERE idea_id=? AND done_at IS NULL`
	}
	_, err := s.DB.ExecContext(ctx, q, next.Unix(), ideaID)
	return err
}

// CloseUrgent: answered (yes | no), cancelled by the author or expired; false — it was not open
func (s *Store) CloseUrgent(ctx context.Context, spaceID, ideaID int64, answer string, now time.Time) (bool, error) {
	r, err := s.DB.ExecContext(ctx, `UPDATE urgent SET done_at=?, answer=? WHERE idea_id=? AND space_id=? AND done_at IS NULL`, now.Unix(), answer, ideaID, spaceID)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}
