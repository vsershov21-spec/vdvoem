package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Option struct {
	ID        int64
	IdeaID    int64
	MemberID  int64 // 0 = the assistant
	Title     string
	URL       string
	Address   string
	ImageURL  string
	Note      string
	PriceKop  int64
	Parsed    bool
	CreatedAt time.Time
}

const optCols = `id, idea_id, COALESCE(member_id,0), title, url, address, image_url, note, price_kop, parsed, created_at`

func scanOpt(sc interface{ Scan(...any) error }) (Option, error) {
	var o Option
	var p int
	var c int64
	err := sc.Scan(&o.ID, &o.IdeaID, &o.MemberID, &o.Title, &o.URL, &o.Address, &o.ImageURL, &o.Note, &o.PriceKop, &p, &c)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	o.Parsed, o.CreatedAt = p == 1, time.Unix(c, 0)
	return o, err
}

func (s *Store) AddOption(ctx context.Context, o Option, now time.Time) (Option, error) {
	var m any
	if o.MemberID != 0 {
		m = o.MemberID
	}
	r, err := s.DB.ExecContext(ctx, `INSERT INTO idea_options(idea_id, member_id, title, url, address, image_url, note, price_kop, parsed, created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		o.IdeaID, m, o.Title, o.URL, o.Address, o.ImageURL, o.Note, o.PriceKop, b2i(o.Parsed), now.Unix())
	if err != nil {
		return o, err
	}
	o.ID, _ = r.LastInsertId()
	o.CreatedAt = now
	return o, nil
}

func (s *Store) UpdateOption(ctx context.Context, o Option) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE idea_options SET title=?, url=?, address=?, image_url=?, note=?, price_kop=?, parsed=? WHERE id=?`,
		o.Title, o.URL, o.Address, o.ImageURL, o.Note, o.PriceKop, b2i(o.Parsed), o.ID)
	return err
}

func (s *Store) Option(ctx context.Context, id int64) (Option, error) {
	return scanOpt(s.DB.QueryRowContext(ctx, `SELECT `+optCols+` FROM idea_options WHERE id=?`, id))
}

func (s *Store) Options(ctx context.Context, ideaID int64) ([]Option, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+optCols+` FROM idea_options WHERE idea_id=? ORDER BY id`, ideaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Option{}
	for rows.Next() {
		o, err := scanOpt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) DeleteOption(ctx context.Context, ideaID, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM idea_options WHERE id=? AND idea_id=?`, id, ideaID)
	if err == nil {
		_, err = s.DB.ExecContext(ctx, `UPDATE ideas SET chosen_option=NULL WHERE id=? AND chosen_option=?`, ideaID, id)
	}
	return err
}

func (s *Store) ChooseOption(ctx context.Context, ideaID, optID int64) error {
	var v any
	if optID != 0 {
		v = optID
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE ideas SET chosen_option=? WHERE id=?`, v, ideaID)
	return err
}

func (s *Store) ChosenOption(ctx context.Context, ideaID int64) (int64, error) {
	var v sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT chosen_option FROM ideas WHERE id=?`, ideaID).Scan(&v)
	return v.Int64, err
}

func (s *Store) SaveCard(ctx context.Context, chatID, msgID, ideaID int64) {
	_, _ = s.DB.ExecContext(ctx, `INSERT OR REPLACE INTO bot_cards(chat_id, message_id, idea_id) VALUES (?,?,?)`, chatID, msgID, ideaID)
}

func (s *Store) CardIdea(ctx context.Context, chatID, msgID int64) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT idea_id FROM bot_cards WHERE chat_id=? AND message_id=?`, chatID, msgID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}
