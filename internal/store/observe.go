package store

import (
	"context"
	"time"
)

// Observability for the owner: the assistant's journal, boot stages, route times — all in the database, so a
// deploy does not wipe them and the admin can read them.

// ExampleMeta: what happened with a request — how long it took, which model read it, what the answer offered
func (s *Store) SetExampleMeta(ctx context.Context, id, ms int64, model, buttons string, cards int, errText string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE llm_examples SET ms=?, model=?, buttons=?, cards=?, err=? WHERE id=?`, ms, model, buttons, cards, errText, id)
	return err
}

type LogRow struct {
	ID      int64  `json:"id"`
	Member  int64  `json:"member"`
	Text    string `json:"text"`
	Intent  string `json:"intent"`
	Answer  string `json:"answer"`
	Outcome string `json:"outcome"`
	Src     string `json:"src"`
	Pressed bool   `json:"pressed"`
	Ms      int64  `json:"ms"`
	Model   string `json:"model"`
	Buttons string `json:"buttons"`
	Cards   int    `json:"cards"`
	Err     string `json:"err"`
	At      int64  `json:"at"`
}

// AssistantLog: the requests since a moment, newest first (labels added by the agent are left out — they are not requests)
func (s *Store) AssistantLog(ctx context.Context, since time.Time, limit int) ([]LogRow, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, member_id, input, calls, answer, outcome, src, accepted, ms, model, buttons, cards, err, created_at
		FROM llm_examples WHERE created_at>=? AND outcome!='labeled' ORDER BY id DESC LIMIT ?`, since.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LogRow{}
	for rows.Next() {
		var r LogRow
		var a int
		if err := rows.Scan(&r.ID, &r.Member, &r.Text, &r.Intent, &r.Answer, &r.Outcome, &r.Src, &a, &r.Ms, &r.Model, &r.Buttons, &r.Cards, &r.Err, &r.At); err != nil {
			return nil, err
		}
		r.Pressed = a == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) AddBoot(ctx context.Context, boot, stage, msg string, ms int, now time.Time) {
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO client_boots(boot, stage, msg, ms, at) VALUES (?,?,?,?,?)`, boot, stage, msg, ms, now.Unix())
}

type BootRow struct {
	Boot  string `json:"boot"`
	Stage string `json:"stage"`
	Msg   string `json:"msg"`
	Ms    int    `json:"ms"`
	At    int64  `json:"at"`
}

func (s *Store) Boots(ctx context.Context, since time.Time, limit int) ([]BootRow, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT boot, stage, msg, ms, at FROM client_boots WHERE at>=? ORDER BY id DESC LIMIT ?`, since.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BootRow{}
	for rows.Next() {
		var b BootRow
		if err := rows.Scan(&b.Boot, &b.Stage, &b.Msg, &b.Ms, &b.At); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) AddRouteTime(ctx context.Context, route string, ms int64, now time.Time) {
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO route_times(route, ms, at) VALUES (?,?,?)`, route, ms, now.Unix())
}

type RouteStat struct {
	Route string `json:"route"`
	N     int    `json:"n"`
	P50   int64  `json:"p50"`
	P90   int64  `json:"p90"`
	Max   int64  `json:"max"`
}

func (s *Store) RouteTimes(ctx context.Context, since time.Time) (map[string][]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT route, ms FROM route_times WHERE at>=? ORDER BY route, ms`, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]int64{}
	for rows.Next() {
		var r string
		var ms int64
		if err := rows.Scan(&r, &ms); err != nil {
			return nil, err
		}
		out[r] = append(out[r], ms)
	}
	return out, rows.Err()
}

// PruneObserve: boots after 30 days, route times after 7
func (s *Store) PruneObserve(ctx context.Context, now time.Time) {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM client_boots WHERE at<?`, now.Add(-30*24*time.Hour).Unix())
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM route_times WHERE at<?`, now.Add(-7*24*time.Hour).Unix())
}
