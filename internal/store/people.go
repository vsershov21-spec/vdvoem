package store

import (
	"context"
	"time"
)

// Memory is a preference a person told the assistant («не люблю стендап»). Personal: only its owner sees and deletes it.
type Memory struct {
	ID, MemberID int64
	Text         string
	CreatedAt    time.Time
}

func (s *Store) AddMemory(ctx context.Context, spaceID, memberID int64, text string, now time.Time) (Memory, error) {
	r, err := s.DB.ExecContext(ctx, `INSERT INTO memories(space_id, member_id, text, created_at) VALUES (?,?,?,?)`, spaceID, memberID, text, now.Unix())
	if err != nil {
		return Memory{}, err
	}
	id, _ := r.LastInsertId()
	return Memory{ID: id, MemberID: memberID, Text: text, CreatedAt: now}, nil
}

// Memories of a space (both people), newest first.
func (s *Store) Memories(ctx context.Context, spaceID int64) ([]Memory, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, member_id, text, created_at FROM memories WHERE space_id=? ORDER BY id DESC LIMIT 60`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Memory{}
	for rows.Next() {
		var m Memory
		var c int64
		if err := rows.Scan(&m.ID, &m.MemberID, &m.Text, &c); err != nil {
			return nil, err
		}
		m.CreatedAt = time.Unix(c, 0)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DeleteMemory(ctx context.Context, memberID, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM memories WHERE id=? AND member_id=?`, id, memberID)
	return err
}

// Goal is something the scout should watch for («театр в январе», «концерт Земфиры»).
type Goal struct {
	ID, MemberID int64
	Text         string
	Done         bool
	CreatedAt    time.Time
}

func (s *Store) AddGoal(ctx context.Context, spaceID, memberID int64, text string, now time.Time) (Goal, error) {
	r, err := s.DB.ExecContext(ctx, `INSERT INTO scout_goals(space_id, member_id, text, created_at) VALUES (?,?,?,?)`, spaceID, memberID, text, now.Unix())
	if err != nil {
		return Goal{}, err
	}
	id, _ := r.LastInsertId()
	return Goal{ID: id, MemberID: memberID, Text: text, CreatedAt: now}, nil
}

func (s *Store) Goals(ctx context.Context, spaceID int64) ([]Goal, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, member_id, text, done, created_at FROM scout_goals WHERE space_id=? AND done=0 ORDER BY id DESC LIMIT 30`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Goal{}
	for rows.Next() {
		var g Goal
		var c int64
		var d int
		if err := rows.Scan(&g.ID, &g.MemberID, &g.Text, &d, &c); err != nil {
			return nil, err
		}
		g.Done, g.CreatedAt = d == 1, time.Unix(c, 0)
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) CloseGoal(ctx context.Context, spaceID, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE scout_goals SET done=1 WHERE id=? AND space_id=?`, id, spaceID)
	return err
}

// ---------- training examples for the local model ----------

func (s *Store) AddExample(ctx context.Context, memberID int64, input, calls, answer string, now time.Time) (int64, error) {
	r, err := s.DB.ExecContext(ctx, `INSERT INTO llm_examples(member_id, input, calls, answer, created_at) VALUES (?,?,?,?,?)`, memberID, input, calls, answer, now.Unix())
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (s *Store) AcceptExample(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE llm_examples SET accepted=1, outcome=CASE WHEN outcome='' THEN 'pressed' ELSE outcome END WHERE id=?`, id)
	return err
}

// SetOutcome: how the request ended (clarified | rephrased | labeled …); a miss is never a training example.
func (s *Store) SetOutcome(ctx context.Context, id int64, outcome, src string) error {
	q := `UPDATE llm_examples SET outcome=?`
	args := []any{outcome}
	if src != "" {
		q += `, src=?`
		args = append(args, src)
	}
	if outcome == "clarified" || outcome == "rephrased" {
		q += `, accepted=0`
	}
	_, err := s.DB.ExecContext(ctx, q+` WHERE id=?`, append(args, id)...)
	return err
}

// LastExample: the member's previous understood request (to notice «asked again in other words»).
func (s *Store) LastExample(ctx context.Context, memberID int64) (Example, error) {
	var e Example
	var a int
	var at int64
	err := s.DB.QueryRowContext(ctx, `SELECT id, input, calls, answer, accepted, outcome, src, created_at FROM llm_examples WHERE member_id=? ORDER BY id DESC LIMIT 1`, memberID).
		Scan(&e.ID, &e.Input, &e.Calls, &e.Answer, &a, &e.Outcome, &e.Src, &at)
	e.Accepted, e.At = a == 1, time.Unix(at, 0)
	return e, err
}

// Misses: requests understood wrong (clarified, rephrased) — to be labeled and trained on.
func (s *Store) Misses(ctx context.Context, since time.Time) ([]Example, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, input, calls, answer, accepted, outcome, src, created_at FROM llm_examples
		WHERE outcome IN ('clarified','rephrased','clarified+labeled','rephrased+labeled') AND created_at>=? ORDER BY id`, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Example
	for rows.Next() {
		var e Example
		var a int
		var at int64
		if err := rows.Scan(&e.ID, &e.Input, &e.Calls, &e.Answer, &a, &e.Outcome, &e.Src, &at); err != nil {
			return nil, err
		}
		e.Accepted, e.At = a == 1, time.Unix(at, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Outcomes: counts per outcome since a moment — the live «understood» rate.
func (s *Store) Outcomes(ctx context.Context, since time.Time) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT outcome, src, COUNT(*) FROM llm_examples WHERE created_at>=? GROUP BY outcome, src`, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var o, src string
		var n int
		if err := rows.Scan(&o, &src, &n); err != nil {
			return nil, err
		}
		if o == "" {
			o = "none"
		}
		out[o] += n
		if src != "" {
			out["src:"+src] += n
		}
	}
	return out, rows.Err()
}

// LabelExample: the right reading of a missed request (by a person or the labeling agent) — a training example
func (s *Store) LabelExample(ctx context.Context, missID int64, calls string, now time.Time) error {
	r, err := s.DB.ExecContext(ctx, `INSERT INTO llm_examples(member_id, input, calls, answer, accepted, outcome, src, created_at)
		SELECT member_id, input, ?, '', 1, 'labeled', 'agent', ? FROM llm_examples WHERE id=?`, calls, now.Unix(), missID)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE llm_examples SET outcome=outcome||'+labeled' WHERE id=? AND outcome NOT LIKE '%+labeled'`, missID)
	return err
}

type Example struct {
	ID                   int64
	Input, Calls, Answer string
	Accepted             bool
	Outcome, Src         string
	At                   time.Time
}

func (s *Store) Examples(ctx context.Context, onlyAccepted bool) ([]Example, error) {
	q := `SELECT input, calls, answer, accepted FROM llm_examples`
	if onlyAccepted {
		q += ` WHERE accepted=1`
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Example
	for rows.Next() {
		var e Example
		var a int
		if err := rows.Scan(&e.Input, &e.Calls, &e.Answer, &a); err != nil {
			return nil, err
		}
		e.Accepted = a == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

// Question for the scout — answered at its next run.
type Question struct {
	ID, SpaceID, MemberID, IdeaID int64
	Text, Answer                  string
	CreatedAt                     time.Time
}

func (s *Store) AddQuestion(ctx context.Context, q Question, now time.Time) (int64, error) {
	var idea any
	if q.IdeaID != 0 {
		idea = q.IdeaID
	}
	r, err := s.DB.ExecContext(ctx, `INSERT INTO scout_questions(space_id, member_id, idea_id, text, created_at) VALUES (?,?,?,?,?)`, q.SpaceID, q.MemberID, idea, q.Text, now.Unix())
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (s *Store) OpenQuestions(ctx context.Context, spaceID int64) ([]Question, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, space_id, member_id, COALESCE(idea_id,0), text, created_at FROM scout_questions WHERE space_id=? AND answered_at IS NULL ORDER BY id LIMIT 20`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Question{}
	for rows.Next() {
		var q Question
		var c int64
		if err := rows.Scan(&q.ID, &q.SpaceID, &q.MemberID, &q.IdeaID, &q.Text, &c); err != nil {
			return nil, err
		}
		q.CreatedAt = time.Unix(c, 0)
		out = append(out, q)
	}
	return out, rows.Err()
}

// AnswerQuestion stores the scout's answer once; returns the question (false if already answered or unknown).
func (s *Store) AnswerQuestion(ctx context.Context, spaceID, id int64, answer string, now time.Time) (Question, bool, error) {
	r, err := s.DB.ExecContext(ctx, `UPDATE scout_questions SET answer=?, answered_at=? WHERE id=? AND space_id=? AND answered_at IS NULL`, answer, now.Unix(), id, spaceID)
	if err != nil {
		return Question{}, false, err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return Question{}, false, nil
	}
	var q Question
	var c int64
	err = s.DB.QueryRowContext(ctx, `SELECT id, space_id, member_id, COALESCE(idea_id,0), text, created_at FROM scout_questions WHERE id=?`, id).Scan(&q.ID, &q.SpaceID, &q.MemberID, &q.IdeaID, &q.Text, &c)
	q.Answer, q.CreatedAt = answer, time.Unix(c, 0)
	return q, err == nil, err
}
