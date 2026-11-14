package store

import (
	"context"
	"database/sql"
	"time"

	"vdvoem/internal/domain"
)

const proposalCols = `id, space_id, kind, ref_id, author_id, title, sub, payload, state, created_at, answered_at, reminded, author_saw`

func scanProposal(r interface{ Scan(...any) error }) (domain.Proposal, error) {
	var p domain.Proposal
	var c int64
	var a sql.NullInt64
	var rem, saw int
	err := r.Scan(&p.ID, &p.SpaceID, &p.Kind, &p.RefID, &p.AuthorID, &p.Title, &p.Sub, &p.Payload, &p.State, &c, &a, &rem, &saw)
	p.CreatedAt, p.AnsweredAt, p.Reminded, p.AuthorSaw = time.Unix(c, 0), tp(a), rem != 0, saw != 0
	return p, err
}

// CreateProposal opens a proposal. A waiting one of the same kind about the same thing is withdrawn first:
// a new time replaces the previous offer, it does not pile up next to it.
func (s *Store) CreateProposal(ctx context.Context, p domain.Proposal, now time.Time) (domain.Proposal, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `UPDATE proposals SET state='withdrawn', answered_at=? WHERE space_id=? AND kind=? AND ref_id=? AND state='wait'`,
		now.Unix(), p.SpaceID, p.Kind, p.RefID); err != nil {
		return p, err
	}
	if p.Payload == "" {
		p.Payload = "{}"
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO proposals(space_id, kind, ref_id, author_id, title, sub, payload, state, created_at) VALUES (?,?,?,?,?,?,?,'wait',?)`,
		p.SpaceID, p.Kind, p.RefID, p.AuthorID, p.Title, p.Sub, p.Payload, now.Unix())
	if err != nil {
		return p, err
	}
	p.ID, _ = res.LastInsertId()
	p.State, p.CreatedAt = domain.ProposalWait, now
	return p, tx.Commit()
}

func (s *Store) Proposal(ctx context.Context, spaceID, id int64) (domain.Proposal, error) {
	p, err := scanProposal(s.DB.QueryRowContext(ctx, `SELECT `+proposalCols+` FROM proposals WHERE id=? AND space_id=?`, id, spaceID))
	if err == sql.ErrNoRows {
		return p, ErrNotFound
	}
	return p, err
}

// LiveProposals: everything a home screen or a card may still show — waiting ones and the «нет» the author
// has not seen yet
func (s *Store) LiveProposals(ctx context.Context, spaceID int64) ([]domain.Proposal, error) {
	return s.proposals(ctx, `SELECT `+proposalCols+` FROM proposals WHERE space_id=? AND (state='wait' OR (state='no' AND author_saw=0)) ORDER BY created_at DESC, id DESC`, spaceID)
}

// WaitingProposals: across all spaces, for the reminder tick
func (s *Store) WaitingProposals(ctx context.Context) ([]domain.Proposal, error) {
	return s.proposals(ctx, `SELECT `+proposalCols+` FROM proposals WHERE state='wait' AND reminded=0 AND kind NOT IN ('cancel','note') ORDER BY id`)
}

func (s *Store) proposals(ctx context.Context, q string, args ...any) ([]domain.Proposal, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Proposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CloseProposal moves a waiting proposal to yes / no / withdrawn. Two phones answering at once: the first
// wins, the second gets ErrNotFound and re-reads.
func (s *Store) CloseProposal(ctx context.Context, spaceID, id int64, st domain.ProposalState, now time.Time) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE proposals SET state=?, answered_at=? WHERE id=? AND space_id=? AND state='wait'`, string(st), now.Unix(), id, spaceID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MarkProposalReminded(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE proposals SET reminded=1 WHERE id=?`, id)
	return err
}

// MarkProposalSeen: the author has seen the «нет» — the row leaves their home screen
func (s *Store) MarkProposalSeen(ctx context.Context, spaceID, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE proposals SET author_saw=1 WHERE id=? AND space_id=?`, id, spaceID)
	return err
}
