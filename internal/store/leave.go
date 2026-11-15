package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"vdvoem/internal/domain"
)

// Leaving a pair and deleting one's data. Both divide what two people have, so every step is in one
// transaction and follows one rule: the one who stays loses nothing.
//
// Leaving: the pair's space stays with the one who stays — ideas (also those the leaver added), marks, the
// history of meetings and chores. The leaver stays the same member (logins, chat, memory, taste, settings)
// and moves into a new space of their own with COPIES of the ideas they added and of the chores that were theirs.
//
// Deleting: first the same leaving, without copies; then every reference to the member is taken out of what
// stays with others (their name, marks and ratings go, the shared things stay), and what was only theirs is
// removed. Row ids are reused by SQLite, so nothing may keep pointing at a deleted member or space: a new
// person would inherit it.

// KeyLeft: who left this space and which side was theirs («12:b»). While the one who stayed is alone the
// marks of that side are kept; they belong to nobody else, so they are wiped when another person takes the side.
const (
	KeyLeft     = "sys.left"
	KeyPairedAt = "sys.paired_at"
)

// GoneMember stands in chores for a partner who has deleted their data (done_by must not point at a row
// that a new person may get)
const GoneMember int64 = -1

type CancelledMeet struct {
	RoundID  int64
	Title    string
	StartsAt time.Time
	Planned  bool
}

// Left: what leaving changed
type Left struct {
	Partner   *Member // the one who stays (nil: the member was alone)
	PairSpace int64
	Cancelled []CancelledMeet
	Ideas     int // copies the leaver got
	Chores    int
}

type execer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

func run(ctx context.Context, tx execer, steps []step) error {
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, st.q, st.args...); err != nil {
			return fmt.Errorf("%s: %w", strings.SplitN(st.q, "\n", 2)[0], err)
		}
	}
	return nil
}

type step struct {
	q    string
	args []any
}

func otherColor(c string) string {
	if c == "a" {
		return "b"
	}
	return "a"
}

// partnerOf: the other member of the space, if there is one
func partnerOf(ctx context.Context, tx execer, me Member) (*Member, error) {
	p, err := scanMember(tx.QueryRowContext(ctx, `SELECT `+memberCols+` FROM members WHERE space_id=? AND id<>?`, me.SpaceID, me.ID))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return &p, err
}

type roundRow struct {
	id   int64
	body string
}

func roundRows(ctx context.Context, tx execer, where string, args ...any) ([]roundRow, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, body FROM rounds WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []roundRow
	for rows.Next() {
		var r roundRow
		if err := rows.Scan(&r.id, &r.body); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// detach: the pair stops being a pair. Meetings that have not happened are cancelled, what waited for an
// answer is withdrawn (nobody is left to answer), a chore of the leaver goes to «кто угодно».
func detach(ctx context.Context, tx execer, me Member, now time.Time) ([]CancelledMeet, error) {
	rs, err := roundRows(ctx, tx, `space_id=? AND state IN ('time','vote','confirm','planned')`, me.SpaceID)
	if err != nil {
		return nil, err
	}
	var out []CancelledMeet
	for _, row := range rs {
		var r domain.Round
		if err := json.Unmarshal([]byte(row.body), &r); err != nil {
			return nil, err
		}
		planned := r.State == domain.StatePlanned
		if planned && !r.StartsAt.After(now) {
			continue // it has happened: this is history, it stays as it is
		}
		if err := r.Cancel(); err != nil {
			return nil, err
		}
		b, _ := json.Marshal(r)
		if _, err := tx.ExecContext(ctx, `UPDATE rounds SET state=?, version=?, body=?, updated_at=? WHERE id=?`, string(r.State), r.Version, string(b), now.Unix(), row.id); err != nil {
			return nil, err
		}
		c := CancelledMeet{RoundID: row.id, StartsAt: r.StartsAt, Planned: planned}
		if r.Chosen != 0 {
			_ = tx.QueryRowContext(ctx, `SELECT title FROM ideas WHERE id=?`, r.Chosen).Scan(&c.Title)
		}
		if c.Title != "" { // an old choice by slots has no idea yet: nothing to name
			out = append(out, c)
		}
	}
	return out, run(ctx, tx, []step{
		{`UPDATE proposals SET state='withdrawn', answered_at=? WHERE space_id=? AND state='wait'`, []any{now.Unix(), me.SpaceID}},
		{`UPDATE proposals SET author_saw=1 WHERE space_id=? AND state='no' AND author_saw=0`, []any{me.SpaceID}},
		{`UPDATE urgent SET done_at=?, answer='cancelled' WHERE space_id=? AND done_at IS NULL`, []any{now.Unix(), me.SpaceID}},
		// a link that was sent while they were a pair must not bring anybody into what is one person's now
		{`UPDATE invites SET expires_at=? WHERE space_id=? AND used_at IS NULL AND expires_at>?`, []any{now.Unix(), me.SpaceID, now.Unix()}},
		{`UPDATE spaces SET status='solo' WHERE id=?`, []any{me.SpaceID}},
	})
}

// freeChores: what was asked or returned becomes plain open, and nothing is left assigned to the one who goes
func freeChores(ctx context.Context, tx execer, me Member) error {
	return run(ctx, tx, []step{
		{`UPDATE chores SET state='open', proposal_id=0 WHERE space_id=? AND state IN ('ask','declined')`, []any{me.SpaceID}},
		{`UPDATE chores SET assignee_id=0 WHERE space_id=? AND assignee_id=? AND state='open'`, []any{me.SpaceID, me.ID}},
		{`UPDATE chores SET assignee_id=0 WHERE space_id=? AND assignee_id=? AND state='open'`, []any{me.SpaceID, domain.ChoreTogether}}, // «вместе» needs two
	})
}

// LeavePair: see the top of the file. ErrNotFound — there is no pair to leave.
func (s *Store) LeavePair(ctx context.Context, memberID int64, now time.Time) (Left, error) {
	var out Left
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	me, err := scanMember(tx.QueryRowContext(ctx, `SELECT `+memberCols+` FROM members WHERE id=?`, memberID))
	if err != nil {
		return out, err
	}
	if out.Partner, err = partnerOf(ctx, tx, me); err != nil {
		return out, err
	}
	if out.Partner == nil {
		return out, ErrNotFound
	}
	pair := me.SpaceID
	out.PairSpace = pair
	if out.Cancelled, err = detach(ctx, tx, me, now); err != nil {
		return out, err
	}
	// a space of their own, with the couple's city, time zone and schedule
	res, err := tx.ExecContext(ctx, `INSERT INTO spaces(city, tz, plan_weekday, plan_hour, budget_kop, status, created_at, suggest_mode, suggest_weekday, suggest_hour)
SELECT city, tz, plan_weekday, plan_hour, budget_kop, 'solo', ?, suggest_mode, suggest_weekday, suggest_hour FROM spaces WHERE id=?`, now.Unix(), pair)
	if err != nil {
		return out, err
	}
	solo, _ := res.LastInsertId()

	// copies of the ideas the leaver added themselves: not chores, not what the scout found, not stops of a
	// route. A field the partner typed is the partner's: it is not copied. The originals are not touched.
	ids, err := int64s(ctx, tx, `SELECT id FROM ideas WHERE space_id=? AND author_id=? AND source='' AND kind<>'chore' AND status IN ('active','draft','done') ORDER BY id`, pair, me.ID)
	if err != nil {
		return out, err
	}
	theirs := func(field string) string {
		return fmt.Sprintf(`EXISTS (SELECT 1 FROM idea_edits e WHERE e.idea_id=ideas.id AND e.field='%s' AND e.member_id<>%d)`, field, me.ID)
	}
	copyIdea := `INSERT INTO ideas(space_id, author_id, title, kind, category, url, address, image_url, note, price_min, price_max, duration_min,
starts_at, ends_at, deadline, assignee, status, fingerprint, wish_a, wish_b, parsed, created_at, updated_at, source, info)
SELECT ?, author_id, title, kind, category, url,
 CASE WHEN ` + theirs("where") + ` THEN '' ELSE address END, image_url,
 CASE WHEN ` + theirs("note") + ` THEN '' ELSE note END,
 CASE WHEN ` + theirs("price") + ` THEN 0 ELSE price_min END, CASE WHEN ` + theirs("price") + ` THEN 0 ELSE price_max END, duration_min,
 CASE WHEN ` + theirs("when") + ` THEN NULL ELSE starts_at END, CASE WHEN ` + theirs("when") + ` THEN NULL ELSE ends_at END,
 deadline, '', status, fingerprint, wish_` + me.Color + `, '', parsed, created_at, ?, '',
 CASE WHEN ` + theirs("about") + ` THEN '' ELSE info END
FROM ideas WHERE id=?`
	for _, id := range ids {
		r, err := tx.ExecContext(ctx, copyIdea, solo, now.Unix(), id)
		if err != nil {
			return out, fmt.Errorf("copy idea: %w", err)
		}
		nid, _ := r.LastInsertId()
		if _, err := tx.ExecContext(ctx, `INSERT INTO idea_edits(idea_id, field, member_id, at) SELECT ?, field, member_id, at FROM idea_edits WHERE idea_id=? AND member_id=?`, nid, id, me.ID); err != nil {
			return out, err
		}
		out.Ideas++
	}
	// the chores that were theirs: a copy goes along, the original stays for «кто угодно»
	r, err := tx.ExecContext(ctx, `INSERT INTO chores(space_id, author_id, assignee_id, title, note, due_at, due_time, repeat, state, created_at)
SELECT ?, ?, ?, title, note, due_at, due_time, repeat, 'open', created_at FROM chores WHERE space_id=? AND assignee_id IN (?,?) AND state IN ('open','ask','declined')`, solo, me.ID, me.ID, pair, me.ID, domain.ChoreTogether)
	if err != nil {
		return out, err
	}
	n, _ := r.RowsAffected()
	out.Chores = int(n)
	if err := freeChores(ctx, tx, me); err != nil {
		return out, err
	}
	err = run(ctx, tx, []step{
		// the member row itself moves: sessions, passkeys, the chat with the bot, taste and settings stay theirs
		{`UPDATE members SET space_id=?, color='a' WHERE id=?`, []any{solo, me.ID}},
		{`UPDATE assistant_messages SET space_id=? WHERE member_id=?`, []any{solo, me.ID}},
		{`UPDATE assistant_actions SET space_id=? WHERE member_id=?`, []any{solo, me.ID}},
		{`UPDATE memories SET space_id=? WHERE member_id=?`, []any{solo, me.ID}},
		{`UPDATE scout_goals SET space_id=? WHERE member_id=?`, []any{solo, me.ID}},
		{`UPDATE scout_questions SET space_id=? WHERE member_id=?`, []any{solo, me.ID}},
		{`UPDATE picks SET space_id=? WHERE member_id=?`, []any{solo, me.ID}},
		{`INSERT INTO space_settings(space_id, key, value) SELECT ?, key, value FROM space_settings WHERE space_id=? AND key NOT LIKE 'sys.%'`, []any{solo, pair}},
		{upsertSpaceSetting, []any{pair, KeyLeft, fmt.Sprintf("%d:%s", me.ID, me.Color)}},
		{upsertSpaceSetting, []any{pair, KeyPairedAt, ""}},
	})
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

const upsertSpaceSetting = `INSERT INTO space_settings(space_id, key, value) VALUES (?,?,?) ON CONFLICT(space_id, key) DO UPDATE SET value=excluded.value`

func int64s(ctx context.Context, tx execer, q string, args ...any) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// clearSide: the marks and ratings of one side of a space go — the person behind them is not there any more
// and somebody else takes the side (or they asked to be forgotten). The ideas and the meetings stay.
func clearSide(ctx context.Context, tx execer, spaceID int64, color string) error {
	if color != "a" && color != "b" {
		return fmt.Errorf("bad side %q", color)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ideas SET wish_`+color+`='' WHERE space_id=? AND wish_`+color+`<>''`, spaceID); err != nil {
		return err
	}
	rs, err := roundRows(ctx, tx, `space_id=?`, spaceID)
	if err != nil {
		return err
	}
	for _, row := range rs {
		var r domain.Round
		if err := json.Unmarshal([]byte(row.body), &r); err != nil {
			return err
		}
		if color == "a" {
			r.AgainA, r.VoteA, r.SlotsA = "", domain.VoteNone, nil
		} else {
			r.AgainB, r.VoteB, r.SlotsB = "", domain.VoteNone, nil
		}
		b, _ := json.Marshal(r)
		if string(b) == row.body {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE rounds SET body=? WHERE id=?`, string(b), row.id); err != nil {
			return err
		}
	}
	return nil
}

// welcome: somebody takes the free side of a space. If the one who left it comes back, their marks are still
// there. Anybody else starts clean: the marks and ratings of the one who left are wiped.
func welcome(ctx context.Context, tx execer, spaceID, memberID int64, color string) error {
	var left string
	err := tx.QueryRowContext(ctx, `SELECT value FROM space_settings WHERE space_id=? AND key=?`, spaceID, KeyLeft).Scan(&left)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && left == "") {
		return nil
	}
	if err != nil {
		return err
	}
	if left != fmt.Sprintf("%d:%s", memberID, color) {
		if err := clearSide(ctx, tx, spaceID, color); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, upsertSpaceSetting, spaceID, KeyLeft, "")
	return err
}

// HasHistory: the space was a pair's — it has meetings, or a partner has left it. Such a space is the one
// that is kept when its person pairs up again.
func (s *Store) HasHistory(ctx context.Context, spaceID int64) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM rounds WHERE space_id=?) + (SELECT COUNT(*) FROM space_settings WHERE space_id=? AND key=? AND value<>'')`,
		spaceID, spaceID, KeyLeft).Scan(&n)
	return n > 0
}

// PairedSince: the day the two became a pair (zero time: not a pair). Pairs made before this was recorded
// count from the day the second one joined.
func (s *Store) PairedSince(ctx context.Context, spaceID int64) time.Time {
	var n, joined int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(joined_at),0) FROM members WHERE space_id=?`, spaceID).Scan(&n, &joined); err != nil || n < 2 {
		return time.Time{}
	}
	var v string
	_ = s.DB.QueryRowContext(ctx, `SELECT value FROM space_settings WHERE space_id=? AND key=?`, spaceID, KeyPairedAt).Scan(&v)
	var at int64
	if _, err := fmt.Sscanf(v, "%d", &at); err == nil && at > 0 {
		return time.Unix(at, 0)
	}
	return time.Unix(joined, 0)
}

// MarkPaired: the pair exists from now
func (s *Store) MarkPaired(ctx context.Context, spaceID int64, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, upsertSpaceSetting, spaceID, KeyPairedAt, fmt.Sprintf("%d", now.Unix()))
	return err
}

// ---------- «Удалить мои данные» ----------

// ErrNotAlone: a space was about to be removed with somebody else in it. Never happens by construction;
// the check is the last guard before rows are deleted by space.
var ErrNotAlone = errors.New("space_not_alone")

// jsonField: a field of a job's payload (NULL when the payload is not JSON)
func jsonField(name string) string {
	return `(CASE WHEN json_valid(payload) THEN json_extract(payload,'$.` + name + `') END)`
}

// ForgetMember removes a person. If they are in a pair they leave it first (the partner keeps the ideas, the
// meetings and the chores; the leaver's name, marks and ratings are taken out). Then everything that was only
// theirs is deleted: their own space with what is in it, chat, memory, taste, settings, busy days, logins,
// the member row. Rows are deleted by the member's id and by the id of the space they are alone in — never
// by the id of a space somebody else is in.
func (s *Store) ForgetMember(ctx context.Context, memberID int64, now time.Time) (Left, error) {
	var out Left
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	me, err := scanMember(tx.QueryRowContext(ctx, `SELECT `+memberCols+` FROM members WHERE id=?`, memberID))
	if err != nil {
		return out, err
	}
	if out.Partner, err = partnerOf(ctx, tx, me); err != nil {
		return out, err
	}
	id := me.ID
	own := int64(0) // the space that is only theirs (0: they are in a pair, its space stays)
	if out.Partner != nil {
		out.PairSpace = me.SpaceID
		if out.Cancelled, err = detach(ctx, tx, me, now); err != nil {
			return out, err
		}
		if err = freeChores(ctx, tx, me); err != nil {
			return out, err
		}
		if err = clearSide(ctx, tx, me.SpaceID, me.Color); err != nil {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, upsertSpaceSetting, me.SpaceID, KeyPairedAt, ""); err != nil {
			return out, err
		}
	} else {
		own = me.SpaceID
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM members WHERE space_id=?`, own).Scan(&n); err != nil {
			return out, err
		}
		if n != 1 {
			return out, ErrNotAlone
		}
		if err = wipeSpace(ctx, tx, own); err != nil {
			return out, err
		}
	}

	// spaces they left earlier still hold their marks while the one who stayed is alone
	rows, err := tx.QueryContext(ctx, `SELECT space_id, value FROM space_settings WHERE key=? AND value LIKE ?`, KeyLeft, fmt.Sprintf("%d:%%", id))
	if err != nil {
		return out, err
	}
	type side struct {
		space int64
		color string
	}
	var sides []side
	for rows.Next() {
		var sp int64
		var v string
		if err = rows.Scan(&sp, &v); err != nil {
			rows.Close()
			return out, err
		}
		if _, c, ok := strings.Cut(v, ":"); ok {
			sides = append(sides, side{sp, c})
		}
	}
	rows.Close()
	for _, sd := range sides {
		if err = clearSide(ctx, tx, sd.space, sd.color); err != nil {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, upsertSpaceSetting, sd.space, KeyLeft, ""); err != nil {
			return out, err
		}
	}

	// what stays with others stops pointing at this member. The one who is in that space becomes the author
	// (a space always has somebody in it; if not, NOT NULL fails and nothing is deleted at all).
	other := `(SELECT MIN(m.id) FROM members m WHERE m.space_id=%s AND m.id<>?)`
	viaRound := fmt.Sprintf(other, `(SELECT r.space_id FROM rounds r WHERE r.id=round_id)`)
	title := `title`
	if len([]rune(strings.TrimSpace(me.Name))) >= 2 {
		title = `replace(title, ?, 'Партнёр')`
	}
	propArgs := []any{id}
	if title != `title` {
		propArgs = []any{me.Name, id}
	}
	err = run(ctx, tx, []step{
		{`UPDATE ideas SET author_id=` + fmt.Sprintf(other, `ideas.space_id`) + ` WHERE author_id=?`, []any{id, id}},
		{`UPDATE photos SET member_id=` + viaRound + ` WHERE member_id=?`, []any{id, id}},
		{`UPDATE round_extras SET proposed_by=` + viaRound + ` WHERE proposed_by=?`, []any{id, id}},
		{`UPDATE idea_options SET member_id=NULL WHERE member_id=?`, []any{id}},
		{`UPDATE chores SET author_id=` + fmt.Sprintf(other, `chores.space_id`) + ` WHERE author_id=?`, []any{id, id}},
		{`UPDATE chores SET assignee_id=0 WHERE assignee_id=?`, []any{id}},
		{`UPDATE chores SET done_by=? WHERE done_by=?`, []any{GoneMember, id}},
		{`UPDATE proposals SET author_id=0, title=` + title + ` WHERE author_id=?`, propArgs},
		{`UPDATE urgent SET by_member=0 WHERE by_member=?`, []any{id}},
		{`UPDATE invites SET author_id=0 WHERE author_id=?`, []any{id}},
		{`UPDATE settings SET value='' WHERE key LIKE 'tickets:%' AND value=?`, []any{fmt.Sprintf("%d", id)}},
		{`DELETE FROM idea_edits WHERE member_id=?`, []any{id}},

		// what was only theirs
		{`DELETE FROM sessions WHERE member_id=?`, []any{id}},
		{`DELETE FROM login_codes WHERE member_id=?`, []any{id}},
		{`DELETE FROM passwords WHERE member_id=?`, []any{id}},
		{`DELETE FROM passkeys WHERE member_id=?`, []any{id}},
		{`DELETE FROM webauthn_challenges WHERE member_id=?`, []any{id}},
		{`DELETE FROM assistant_messages WHERE member_id=?`, []any{id}},
		{`DELETE FROM assistant_actions WHERE member_id=?`, []any{id}},
		{`DELETE FROM memories WHERE member_id=?`, []any{id}},
		{`DELETE FROM scout_goals WHERE member_id=?`, []any{id}},
		{`DELETE FROM scout_questions WHERE member_id=?`, []any{id}},
		{`DELETE FROM llm_examples WHERE member_id=?`, []any{id}},
		{`DELETE FROM picks WHERE member_id=?`, []any{id}},
		{`DELETE FROM member_settings WHERE member_id=?`, []any{id}},
		{`DELETE FROM busy_days WHERE member_id=?`, []any{id}},
		{`DELETE FROM free_slots WHERE member_id=?`, []any{id}},
		{`DELETE FROM bot_cards WHERE chat_id=?`, []any{me.ChatID}},
		{`DELETE FROM settings WHERE key IN (?,?,?,?,?)`, []any{fmt.Sprintf("taste:%d", id), fmt.Sprintf("busyurl:%d", id), fmt.Sprintf("cal:%d", id),
			fmt.Sprintf("calseen:%d", id), fmt.Sprintf("tickets_wait:%d", id)}},
		{`DELETE FROM settings WHERE key LIKE 'caltoken:%' AND value=?`, []any{fmt.Sprintf("%d", id)}},
		{`DELETE FROM jobs WHERE ` + jsonField("member") + `=? OR ` + jsonField("author") + `=? OR ` + jsonField("notify") + `=? OR ` + jsonField("chat") + `=?`,
			[]any{id, id, me.ChatID, me.ChatID}},
		{`DELETE FROM members WHERE id=?`, []any{id}},
	})
	if err != nil {
		return out, err
	}
	if own != 0 {
		if _, err = tx.ExecContext(ctx, `DELETE FROM spaces WHERE id=?`, own); err != nil {
			return out, err
		}
	}
	return out, tx.Commit()
}

// wipeSpace removes what is in a space one person is alone in (the space row itself goes after its member)
func wipeSpace(ctx context.Context, tx execer, sp int64) error {
	ideas := `(SELECT id FROM ideas WHERE space_id=?)`
	rounds := `(SELECT id FROM rounds WHERE space_id=?)`
	pre := fmt.Sprintf("%d:", sp)
	return run(ctx, tx, []step{
		{`DELETE FROM jobs WHERE ` + jsonField("space") + `=? OR ` + jsonField("idea") + ` IN ` + ideas + ` OR ` + jsonField("round") + ` IN ` + rounds, []any{sp, sp, sp}},
		{`DELETE FROM idea_edits WHERE idea_id IN ` + ideas, []any{sp}},
		{`DELETE FROM idea_options WHERE idea_id IN ` + ideas, []any{sp}},
		{`DELETE FROM bot_cards WHERE idea_id IN ` + ideas, []any{sp}},
		{`DELETE FROM round_extras WHERE round_id IN ` + rounds + ` OR idea_id IN ` + ideas, []any{sp, sp}},
		{`DELETE FROM photos WHERE round_id IN ` + rounds, []any{sp}},
		{`DELETE FROM urgent WHERE space_id=? OR idea_id IN ` + ideas, []any{sp, sp}},
		{`DELETE FROM settings WHERE key LIKE ? OR key LIKE ? OR (key LIKE 'ticketstoken:%' AND value LIKE ?)`, []any{"tickets:" + pre + "%", "ticketsfile:" + pre + "%", pre + "%"}},
		{`DELETE FROM rounds WHERE space_id=?`, []any{sp}},
		{`DELETE FROM ideas WHERE space_id=?`, []any{sp}},
		{`DELETE FROM invites WHERE space_id=?`, []any{sp}},
		{`DELETE FROM proposals WHERE space_id=?`, []any{sp}},
		{`DELETE FROM chores WHERE space_id=?`, []any{sp}},
		{`DELETE FROM picks WHERE space_id=?`, []any{sp}},
		{`DELETE FROM scout_runs WHERE space_id=?`, []any{sp}},
		{`DELETE FROM scout_goals WHERE space_id=?`, []any{sp}},
		{`DELETE FROM scout_questions WHERE space_id=?`, []any{sp}},
		{`DELETE FROM memories WHERE space_id=?`, []any{sp}},
		{`DELETE FROM assistant_messages WHERE space_id=?`, []any{sp}},
		{`DELETE FROM assistant_actions WHERE space_id=?`, []any{sp}},
		{`DELETE FROM space_settings WHERE space_id=?`, []any{sp}},
	})
}
