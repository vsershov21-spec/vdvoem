package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"vdvoem/internal/domain"
)

const choreCols = `id, space_id, author_id, assignee_id, title, note, due_at, due_time, repeat, state, proposal_id, done_by, done_at, created_at`

func scanChore(r interface{ Scan(...any) error }) (domain.Chore, error) {
	var c domain.Chore
	var due, done sql.NullInt64
	var dt int
	var cr int64
	err := r.Scan(&c.ID, &c.SpaceID, &c.AuthorID, &c.Assignee, &c.Title, &c.Note, &due, &dt, &c.Repeat, &c.State, &c.ProposalID, &c.DoneBy, &done, &cr)
	c.Due, c.DueTime, c.DoneAt, c.CreatedAt = tp(due), dt != 0, tp(done), time.Unix(cr, 0)
	return c, err
}

func (s *Store) CreateChore(ctx context.Context, c domain.Chore, now time.Time) (domain.Chore, error) {
	if c.State == "" {
		c.State = domain.ChoreOpen
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO chores(space_id, author_id, assignee_id, title, note, due_at, due_time, repeat, state, proposal_id, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		c.SpaceID, c.AuthorID, c.Assignee, c.Title, c.Note, nt(c.Due), b2i(c.DueTime), c.Repeat, string(c.State), c.ProposalID, now.Unix())
	if err != nil {
		return c, err
	}
	c.ID, _ = res.LastInsertId()
	c.CreatedAt = now
	return c, nil
}

func (s *Store) Chore(ctx context.Context, spaceID, id int64) (domain.Chore, error) {
	c, err := scanChore(s.DB.QueryRowContext(ctx, `SELECT `+choreCols+` FROM chores WHERE id=? AND space_id=?`, id, spaceID))
	if err == sql.ErrNoRows {
		return c, ErrNotFound
	}
	return c, err
}

// SaveChore writes everything that can change about a chore
func (s *Store) SaveChore(ctx context.Context, c domain.Chore) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE chores SET author_id=?, assignee_id=?, title=?, note=?, due_at=?, due_time=?, repeat=?, state=?, proposal_id=?, done_by=?, done_at=? WHERE id=? AND space_id=?`,
		c.AuthorID, c.Assignee, c.Title, c.Note, nt(c.Due), b2i(c.DueTime), c.Repeat, string(c.State), c.ProposalID, c.DoneBy, nt(c.DoneAt), c.ID, c.SpaceID)
	return err
}

// Chores of a space: everything except deleted ones, the newest done first
func (s *Store) Chores(ctx context.Context, spaceID int64) ([]domain.Chore, error) {
	return s.chores(ctx, `SELECT `+choreCols+` FROM chores WHERE space_id=? AND state<>'deleted' ORDER BY id`, spaceID)
}

// DoneChores: the history — who did what and when; q filters by a word of the title, by — by who did it
func (s *Store) DoneChores(ctx context.Context, spaceID int64, q string, by int64, limit int) ([]domain.Chore, error) {
	sqlq := `SELECT ` + choreCols + ` FROM chores WHERE space_id=? AND state='done'`
	args := []any{spaceID}
	if by > 0 {
		sqlq += ` AND done_by=?`
		args = append(args, by)
	}
	cs, err := s.chores(ctx, sqlq+` ORDER BY done_at DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	// SQLite's LIKE does not fold Cyrillic case: the word is matched here
	out := cs[:0]
	for _, c := range cs {
		if q == "" || containsFold(c.Title, q) {
			out = append(out, c)
			if limit > 0 && len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

// OpenChoresAll: every open chore with a deadline, across spaces — for the morning message
func (s *Store) OpenChoresAll(ctx context.Context) ([]domain.Chore, error) {
	return s.chores(ctx, `SELECT `+choreCols+` FROM chores WHERE state='open' AND due_at IS NOT NULL ORDER BY space_id, due_at`)
}

func (s *Store) chores(ctx context.Context, q string, args ...any) ([]domain.Chore, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Chore{}
	for rows.Next() {
		c, err := scanChore(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------- settings: mine and the couple's ----------

func (s *Store) MemberSettings(ctx context.Context, memberID int64) map[string]string {
	return s.kv(ctx, `SELECT key, value FROM member_settings WHERE member_id=?`, memberID)
}

func (s *Store) SpaceSettings(ctx context.Context, spaceID int64) map[string]string {
	return s.kv(ctx, `SELECT key, value FROM space_settings WHERE space_id=?`, spaceID)
}

func (s *Store) SetMemberSetting(ctx context.Context, memberID int64, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO member_settings(member_id, key, value) VALUES (?,?,?) ON CONFLICT(member_id, key) DO UPDATE SET value=excluded.value`, memberID, key, value)
	return err
}

func (s *Store) SetSpaceSetting(ctx context.Context, spaceID int64, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO space_settings(space_id, key, value) VALUES (?,?,?) ON CONFLICT(space_id, key) DO UPDATE SET value=excluded.value`, spaceID, key, value)
	return err
}

func (s *Store) kv(ctx context.Context, q string, id int64) map[string]string {
	out := map[string]string{}
	rows, err := s.DB.QueryContext(ctx, q, id)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			out[k] = v
		}
	}
	return out
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(strings.TrimSpace(sub)))
}
