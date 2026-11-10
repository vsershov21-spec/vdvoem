package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type ChatMsg struct {
	ID        int64
	Role      string // user | assistant
	Body      json.RawMessage
	CreatedAt time.Time
}

func (s *Store) AddChat(ctx context.Context, spaceID, memberID int64, role string, body any, now time.Time) (int64, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO assistant_messages(space_id, member_id, role, body, created_at) VALUES (?,?,?,?,?)`,
		spaceID, memberID, role, string(b), now.Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Chat returns the latest messages of one member, oldest first.
func (s *Store) Chat(ctx context.Context, memberID int64, limit int) ([]ChatMsg, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, role, body, created_at FROM
		(SELECT * FROM assistant_messages WHERE member_id = ? ORDER BY id DESC LIMIT ?) ORDER BY id`, memberID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatMsg
	for rows.Next() {
		var m ChatMsg
		var body string
		var at int64
		if err := rows.Scan(&m.ID, &m.Role, &body, &at); err != nil {
			return nil, err
		}
		m.Body, m.CreatedAt = json.RawMessage(body), time.Unix(at, 0)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) ClearChat(ctx context.Context, memberID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM assistant_messages WHERE member_id = ?`, memberID)
	return err
}

// Daily count of user questions, for the per-member limit.
func (s *Store) ChatCountSince(ctx context.Context, memberID int64, since time.Time) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM assistant_messages WHERE member_id = ? AND role = 'user' AND created_at >= ?`,
		memberID, since.Unix()).Scan(&n)
	return n, err
}

type Action struct {
	ID       string
	SpaceID  int64
	MemberID int64
	Kind     string
	Payload  json.RawMessage
	Example  int64 // the model exchange that proposed it (0 = rules)
}

func (s *Store) SaveAction(ctx context.Context, a Action, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO assistant_actions(id, space_id, member_id, kind, payload, created_at) VALUES (?,?,?,?,?,?)`,
		a.ID, a.SpaceID, a.MemberID, a.Kind, string(a.Payload), now.Unix())
	return err
}

var ErrUsed = errors.New("store: action already used")

// TakeAction marks an action used and returns it; it works once, only for its owner, within 7 days.
func (s *Store) TakeAction(ctx context.Context, id string, memberID int64, now time.Time) (Action, error) {
	var a Action
	var p string
	// «open a screen» buttons are a way, not an action: they work any number of times, any time later
	err := s.DB.QueryRowContext(ctx, `UPDATE assistant_actions SET used_at = ? WHERE id = ? AND member_id = ? AND (kind = 'open' OR used_at IS NULL AND created_at >= ?)
		RETURNING id, space_id, member_id, kind, payload, example_id`, now.Unix(), id, memberID, now.Add(-7*24*time.Hour).Unix()).Scan(&a.ID, &a.SpaceID, &a.MemberID, &a.Kind, &p, &a.Example)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrUsed
	}
	a.Payload = json.RawMessage(p)
	return a, err
}

// TagAction links a proposed button to the model exchange it came from.
func (s *Store) TagAction(ctx context.Context, id string, example int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE assistant_actions SET example_id=? WHERE id=?`, example, id)
	return err
}
