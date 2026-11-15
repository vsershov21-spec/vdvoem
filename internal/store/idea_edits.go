package store

import (
	"context"
	"time"
)

// IdeaEdit: a field of an idea a member filled by hand
type IdeaEdit struct {
	Field    string
	MemberID int64
	At       time.Time
}

func (s *Store) MarkIdeaEdit(ctx context.Context, ideaID int64, field string, memberID int64, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO idea_edits(idea_id, field, member_id, at) VALUES (?,?,?,?) ON CONFLICT(idea_id, field) DO UPDATE SET member_id=excluded.member_id, at=excluded.at`,
		ideaID, field, memberID, now.Unix())
	return err
}

func (s *Store) IdeaEdits(ctx context.Context, ideaID int64) map[string]IdeaEdit {
	out := map[string]IdeaEdit{}
	rows, err := s.DB.QueryContext(ctx, `SELECT field, member_id, at FROM idea_edits WHERE idea_id=?`, ideaID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var e IdeaEdit
		var at int64
		if rows.Scan(&e.Field, &e.MemberID, &at) == nil {
			e.At = time.Unix(at, 0)
			out[e.Field] = e
		}
	}
	return out
}
