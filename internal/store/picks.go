package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type Pick struct {
	ID, SpaceID, MemberID int64
	Title                 string
	IDs                   []int64
	CreatedAt             time.Time
}

func (s *Store) AddPick(ctx context.Context, p Pick, now time.Time) (int64, error) {
	b, _ := json.Marshal(p.IDs)
	r, err := s.DB.ExecContext(ctx, `INSERT INTO picks(space_id, member_id, title, ids, created_at) VALUES (?,?,?,?,?)`, p.SpaceID, p.MemberID, p.Title, string(b), now.Unix())
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (s *Store) Pick(ctx context.Context, spaceID, id int64) (Pick, error) {
	var p Pick
	var ids string
	var at int64
	err := s.DB.QueryRowContext(ctx, `SELECT id, space_id, member_id, title, ids, created_at FROM picks WHERE id=? AND space_id=?`, id, spaceID).
		Scan(&p.ID, &p.SpaceID, &p.MemberID, &p.Title, &ids, &at)
	if err == sql.ErrNoRows {
		return p, ErrNotFound
	}
	_ = json.Unmarshal([]byte(ids), &p.IDs)
	p.CreatedAt = time.Unix(at, 0)
	return p, err
}
