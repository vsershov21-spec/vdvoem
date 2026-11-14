// Package store keeps «Вдвоём» in SQLite through database/sql. Every query that reads couple data
// is scoped by space_id, so one couple never sees another's rows.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"vdvoem/internal/domain"
	"vdvoem/migrations"
)

var (
	ErrNotFound = errors.New("not_found")
	ErrConflict = errors.New("conflict")
	ErrFull     = errors.New("space_full")
)

type Store struct{ DB *sql.DB }

func Open(db *sql.DB) (*Store, error) {
	s := &Store{DB: db}
	for _, p := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if _, err := db.Exec(p); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	return s, s.migrate()
}

func (s *Store) migrate() error {
	names, _ := fs.Glob(migrations.FS, "*.sql")
	sort.Strings(names)
	var cur int
	_ = s.DB.QueryRow(`SELECT COALESCE(MAX(v),0) FROM schema_version`).Scan(&cur)
	for i, n := range names {
		if i+1 <= cur {
			continue
		}
		b, _ := migrations.FS.ReadFile(n)
		if _, err := s.DB.Exec(string(b)); err != nil {
			return fmt.Errorf("migration %s: %w", n, err)
		}
		if _, err := s.DB.Exec(`INSERT INTO schema_version(v) VALUES (?)`, i+1); err != nil {
			return err
		}
	}
	return nil
}

// ---------- spaces & members ----------

type Space struct {
	ID             int64
	City, TZ       string
	PlanWeekday    int
	SuggestMode    string
	SuggestWeekday int
	SuggestHour    int
	PlanHour       int
	BudgetKop      int64
	Status         string
}

func (sp Space) Loc() *time.Location {
	if l, err := time.LoadLocation(sp.TZ); err == nil {
		return l
	}
	return time.FixedZone("MSK", 3*3600)
}

type Member struct {
	ID            int64
	SpaceID       int64
	TGUserID      int64
	ChatID        int64
	Name          string
	Color         string
	PhotoURL      string
	BiometricLock bool
	Theme         string
}

func (s *Store) CreateSpace(ctx context.Context, now time.Time) (Space, error) {
	r, err := s.DB.ExecContext(ctx, `INSERT INTO spaces(created_at) VALUES (?)`, now.Unix())
	if err != nil {
		return Space{}, err
	}
	id, _ := r.LastInsertId()
	return s.Space(ctx, id)
}

func (s *Store) Space(ctx context.Context, id int64) (Space, error) {
	var sp Space
	err := s.DB.QueryRowContext(ctx, `SELECT id, city, tz, plan_weekday, plan_hour, budget_kop, status, suggest_mode, suggest_weekday, suggest_hour FROM spaces WHERE id=?`, id).
		Scan(&sp.ID, &sp.City, &sp.TZ, &sp.PlanWeekday, &sp.PlanHour, &sp.BudgetKop, &sp.Status, &sp.SuggestMode, &sp.SuggestWeekday, &sp.SuggestHour)
	if errors.Is(err, sql.ErrNoRows) {
		return sp, ErrNotFound
	}
	return sp, err
}

func (s *Store) Spaces(ctx context.Context) ([]Space, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, city, tz, plan_weekday, plan_hour, budget_kop, status, suggest_mode, suggest_weekday, suggest_hour FROM spaces WHERE status='paired'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Space
	for rows.Next() {
		var sp Space
		if err := rows.Scan(&sp.ID, &sp.City, &sp.TZ, &sp.PlanWeekday, &sp.PlanHour, &sp.BudgetKop, &sp.Status, &sp.SuggestMode, &sp.SuggestWeekday, &sp.SuggestHour); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func (s *Store) UpdateSpace(ctx context.Context, sp Space) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE spaces SET city=?, tz=?, plan_weekday=?, plan_hour=?, budget_kop=?, status=?, suggest_mode=?, suggest_weekday=?, suggest_hour=? WHERE id=?`,
		sp.City, sp.TZ, sp.PlanWeekday, sp.PlanHour, sp.BudgetKop, sp.Status, sp.SuggestMode, sp.SuggestWeekday, sp.SuggestHour, sp.ID)
	return err
}

func (s *Store) AddMember(ctx context.Context, m Member, now time.Time) (Member, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return m, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM members WHERE space_id=?`, m.SpaceID).Scan(&n); err != nil {
		return m, err
	}
	if n >= 2 {
		return m, ErrFull
	}
	m.Color = "a"
	if n == 1 {
		m.Color = "b"
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO members(space_id, tg_user_id, chat_id, name, color, photo_url, joined_at) VALUES (?,?,?,?,?,?,?)`,
		m.SpaceID, m.TGUserID, m.ChatID, m.Name, m.Color, m.PhotoURL, now.Unix())
	if err != nil {
		return m, err
	}
	m.ID, _ = r.LastInsertId()
	if n == 1 {
		if _, err := tx.ExecContext(ctx, `UPDATE spaces SET status='paired' WHERE id=?`, m.SpaceID); err != nil {
			return m, err
		}
	}
	return m, tx.Commit()
}

const memberCols = `id, space_id, tg_user_id, chat_id, name, color, photo_url, biometric_lock, theme`

func scanMember(sc interface{ Scan(...any) error }) (Member, error) {
	var m Member
	var lock int
	err := sc.Scan(&m.ID, &m.SpaceID, &m.TGUserID, &m.ChatID, &m.Name, &m.Color, &m.PhotoURL, &lock, &m.Theme)
	m.BiometricLock = lock == 1
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

func (s *Store) MemberByTG(ctx context.Context, tgID int64) (Member, error) {
	return scanMember(s.DB.QueryRowContext(ctx, `SELECT `+memberCols+` FROM members WHERE tg_user_id=?`, tgID))
}

func (s *Store) Members(ctx context.Context, spaceID int64) ([]Member, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+memberCols+` FROM members WHERE space_id=? ORDER BY color`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) UpdateMemberProfile(ctx context.Context, id int64, name, photo string, chatID int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE members SET name=?, photo_url=?, chat_id=? WHERE id=?`, name, photo, chatID, id)
	return err
}

// ---------- invites ----------

func HashCode(code string) string {
	h := sha256.Sum256([]byte("vdvoem-invite:" + code))
	return hex.EncodeToString(h[:])
}

func (s *Store) CreateInvite(ctx context.Context, spaceID int64, code string, expires time.Time) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO invites(code_hash, space_id, expires_at) VALUES (?,?,?)`, HashCode(code), spaceID, expires.Unix())
	return err
}

// UseInvite marks the code used and returns its space. Expired or used codes are ErrNotFound.
func (s *Store) UseInvite(ctx context.Context, code string, now time.Time) (int64, error) {
	var sp int64
	err := s.DB.QueryRowContext(ctx, `UPDATE invites SET used_at=? WHERE code_hash=? AND used_at IS NULL AND expires_at>? RETURNING space_id`,
		now.Unix(), HashCode(code), now.Unix()).Scan(&sp)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return sp, err
}

// ---------- ideas ----------

const ideaCols = `id, space_id, author_id, title, kind, category, url, address, image_url, note, price_min, price_max, duration_min,
starts_at, ends_at, deadline, assignee, status, fingerprint, wish_a, wish_b, again_yes, again_no, ever_shown, last_met_at, parsed, created_at, source, why, horizon, on_sale, info`

type Idea struct {
	domain.Idea
	Note    string
	Parsed  bool
	Source  string     // "" — added by a person, "scout" — found by the afisha agent
	Why     string     // the scout's reason, in a sentence
	Horizon string     // scout: week | later (announcements 1–3 months ahead)
	OnSale  *time.Time // scout: when tickets go on sale
	Info    Info       // what it is: description, genres, cast, age
}

// Info: facts about an event, each from a source (a post, the event's page) — never made up.
type Info struct {
	About  string   `json:"about,omitempty"`  // 1–3 sentences
	Genres []string `json:"genres,omitempty"` // комедия, драма, мюзикл…
	Cast   []string `json:"cast,omitempty"`   // who plays / performs
	Age    string   `json:"age,omitempty"`    // 12+
	Src    string   `json:"src,omitempty"`    // where it came from: post | web | page
}

func (i Info) Empty() bool {
	return i.About == "" && len(i.Genres) == 0 && len(i.Cast) == 0 && i.Age == ""
}

func infoJSON(i Info) string {
	if i.Empty() {
		return ""
	}
	b, _ := json.Marshal(i)
	return string(b)
}

func tp(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.Unix(v.Int64, 0)
	return &t
}

func nt(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}

func scanIdea(sc interface{ Scan(...any) error }) (Idea, error) {
	var i Idea
	var st, en, dl, lm, os sql.NullInt64
	var kind, status, wa, wb string
	var ever, parsed int
	var created int64
	var info string
	err := sc.Scan(&i.ID, &i.SpaceID, &i.AuthorID, &i.Title, &kind, &i.Category, &i.URL, &i.Address, &i.ImageURL, &i.Note,
		&i.PriceMin, &i.PriceMax, &i.DurationMin, &st, &en, &dl, &i.Assignee, &status, &i.Fingerprint, &wa, &wb,
		&i.AgainYes, &i.AgainNo, &ever, &lm, &parsed, &created, &i.Source, &i.Why, &i.Horizon, &os, &info)
	if errors.Is(err, sql.ErrNoRows) {
		return i, ErrNotFound
	}
	i.Kind, i.Status, i.WishA, i.WishB = domain.IdeaKind(kind), domain.IdeaStatus(status), domain.Wish(wa), domain.Wish(wb)
	i.StartsAt, i.EndsAt, i.Deadline, i.LastMetAt = tp(st), tp(en), tp(dl), tp(lm)
	i.EverShown, i.Parsed = ever == 1, parsed == 1
	i.OnSale = tp(os)
	if info != "" {
		_ = json.Unmarshal([]byte(info), &i.Info)
	}
	i.CreatedAt = time.Unix(created, 0)
	return i, err
}

func (s *Store) CreateIdea(ctx context.Context, i Idea, now time.Time) (Idea, error) {
	if i.Kind == "" {
		i.Kind = domain.KindLeisure
	}
	if i.Status == "" {
		i.Status = domain.IdeaActive
	}
	if i.Category == "" {
		i.Category = "other"
	}
	i.Fingerprint = domain.Fingerprint(i.URL, i.Title)
	r, err := s.DB.ExecContext(ctx, `INSERT INTO ideas(space_id, author_id, title, kind, category, url, address, image_url, note, price_min, price_max,
duration_min, starts_at, ends_at, deadline, assignee, status, fingerprint, wish_a, wish_b, created_at, updated_at, source, why, parsed, horizon, on_sale, info)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		i.SpaceID, i.AuthorID, i.Title, string(i.Kind), i.Category, i.URL, i.Address, i.ImageURL, i.Note, i.PriceMin, i.PriceMax,
		i.DurationMin, nt(i.StartsAt), nt(i.EndsAt), nt(i.Deadline), i.Assignee, string(i.Status), i.Fingerprint, string(i.WishA), string(i.WishB),
		now.Unix(), now.Unix(), i.Source, i.Why, b2i(i.Parsed), i.Horizon, nt(i.OnSale), infoJSON(i.Info))
	if err != nil {
		return i, err
	}
	i.ID, _ = r.LastInsertId()
	i.CreatedAt = now
	return i, nil
}

// UpdateIdea rewrites editable fields; wishes and history are changed by their own methods.
func (s *Store) UpdateIdea(ctx context.Context, i Idea, now time.Time) error {
	if i.Fingerprint == "" || strings.HasPrefix(i.Fingerprint, "t:") {
		i.Fingerprint = domain.Fingerprint(i.URL, i.Title)
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE ideas SET title=?, kind=?, category=?, url=?, address=?, image_url=?, note=?, price_min=?, price_max=?,
duration_min=?, starts_at=?, ends_at=?, deadline=?, assignee=?, status=?, fingerprint=?, parsed=?, info=?, updated_at=? WHERE id=? AND space_id=?`,
		i.Title, string(i.Kind), i.Category, i.URL, i.Address, i.ImageURL, i.Note, i.PriceMin, i.PriceMax, i.DurationMin,
		nt(i.StartsAt), nt(i.EndsAt), nt(i.Deadline), i.Assignee, string(i.Status), i.Fingerprint, b2i(i.Parsed), infoJSON(i.Info), now.Unix(), i.ID, i.SpaceID)
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) Idea(ctx context.Context, spaceID, id int64) (Idea, error) {
	return scanIdea(s.DB.QueryRowContext(ctx, `SELECT `+ideaCols+` FROM ideas WHERE id=? AND space_id=?`, id, spaceID))
}

// IdeaAny is for background jobs that only hold the idea id.
func (s *Store) IdeaAny(ctx context.Context, id int64) (Idea, error) {
	return scanIdea(s.DB.QueryRowContext(ctx, `SELECT `+ideaCols+` FROM ideas WHERE id=?`, id))
}

// SetIdeaSource: where an idea came from ("route" — a stop made inside a meeting's route; such ideas are
// left out of every list, see Ideas)
func (s *Store) SetIdeaSource(ctx context.Context, spaceID, id int64, src string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE ideas SET source=? WHERE space_id=? AND id=?`, src, spaceID, id)
	return err
}

func (s *Store) Ideas(ctx context.Context, spaceID int64, statuses ...domain.IdeaStatus) ([]Idea, error) {
	if len(statuses) == 0 {
		statuses = []domain.IdeaStatus{domain.IdeaActive}
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",")
	args := []any{spaceID}
	for _, st := range statuses {
		args = append(args, string(st))
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+ideaCols+` FROM ideas WHERE space_id=? AND status IN (`+ph+`) AND source<>'route' ORDER BY created_at DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Idea
	for rows.Next() {
		i, err := scanIdea(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) IdeaByFingerprint(ctx context.Context, spaceID int64, fp string) (Idea, error) {
	return scanIdea(s.DB.QueryRowContext(ctx, `SELECT `+ideaCols+` FROM ideas WHERE space_id=? AND fingerprint=? AND status IN ('active','draft') ORDER BY id LIMIT 1`, spaceID, fp))
}

func (s *Store) SetWish(ctx context.Context, spaceID, ideaID int64, color string, w domain.Wish, now time.Time) error {
	col := "wish_a"
	if color == "b" {
		col = "wish_b"
	}
	r, err := s.DB.ExecContext(ctx, `UPDATE ideas SET `+col+`=?, updated_at=? WHERE id=? AND space_id=?`, string(w), now.Unix(), ideaID, spaceID)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MarkShown(ctx context.Context, spaceID int64, ids []int64) error {
	for _, id := range ids {
		if _, err := s.DB.ExecContext(ctx, `UPDATE ideas SET ever_shown=1 WHERE id=? AND space_id=?`, id, spaceID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) AfterMeeting(ctx context.Context, spaceID, ideaID int64, metAt time.Time, again string) error {
	q := `UPDATE ideas SET last_met_at=? WHERE id=? AND space_id=?`
	switch again {
	case "yes":
		q = `UPDATE ideas SET last_met_at=?, again_yes=again_yes+1 WHERE id=? AND space_id=?`
	case "no":
		q = `UPDATE ideas SET last_met_at=?, again_no=again_no+1 WHERE id=? AND space_id=?`
	}
	_, err := s.DB.ExecContext(ctx, q, metAt.Unix(), ideaID, spaceID)
	return err
}

func (s *Store) DeleteIdea(ctx context.Context, spaceID, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE ideas SET status='archived' WHERE id=? AND space_id=?`, id, spaceID)
	return err
}

// ---------- rounds ----------

func (s *Store) CreateRound(ctx context.Context, r domain.Round, now time.Time) (domain.Round, error) {
	b, _ := json.Marshal(r)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO rounds(space_id, state, version, body, updated_at) VALUES (?,?,?,?,?)`,
		r.SpaceID, string(r.State), r.Version, string(b), now.Unix())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return r, ErrConflict
		}
		return r, err
	}
	r.ID, _ = res.LastInsertId()
	return r, s.SaveRound(ctx, r, r.Version, now) // store the id inside the body
}

func (s *Store) SaveRound(ctx context.Context, r domain.Round, expectVersion int, now time.Time) error {
	b, _ := json.Marshal(r)
	var starts any
	if !r.StartsAt.IsZero() {
		starts = r.StartsAt.Unix()
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE rounds SET state=?, version=?, body=?, starts_at=?, updated_at=? WHERE id=? AND space_id=? AND version=?`,
		string(r.State), r.Version, string(b), starts, now.Unix(), r.ID, r.SpaceID, expectVersion)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	return nil
}

func scanRound(sc interface{ Scan(...any) error }) (domain.Round, error) {
	var body string
	var r domain.Round
	if err := sc.Scan(&body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return r, ErrNotFound
		}
		return r, err
	}
	err := json.Unmarshal([]byte(body), &r)
	return r, err
}

func (s *Store) Round(ctx context.Context, spaceID, id int64) (domain.Round, error) {
	return scanRound(s.DB.QueryRowContext(ctx, `SELECT body FROM rounds WHERE id=? AND space_id=?`, id, spaceID))
}

func (s *Store) OpenRound(ctx context.Context, spaceID int64) (domain.Round, error) {
	return scanRound(s.DB.QueryRowContext(ctx, `SELECT body FROM rounds WHERE space_id=? AND state IN ('time','vote','confirm') LIMIT 1`, spaceID))
}

// NextPlanned is the nearest planned meeting (or the latest one that has not been closed yet).
func (s *Store) NextPlanned(ctx context.Context, spaceID int64) (domain.Round, error) {
	return scanRound(s.DB.QueryRowContext(ctx, `SELECT body FROM rounds WHERE space_id=? AND state='planned' ORDER BY starts_at LIMIT 1`, spaceID))
}

func (s *Store) RecentRounds(ctx context.Context, spaceID int64, limit int) ([]domain.Round, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT body FROM rounds WHERE space_id=? ORDER BY id DESC LIMIT ?`, spaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Round
	for rows.Next() {
		r, err := scanRound(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ActiveRounds: every round a background tick may need to move (open or planned).
func (s *Store) ActiveRounds(ctx context.Context) ([]domain.Round, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT body FROM rounds WHERE state IN ('time','vote','confirm','planned')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Round
	for rows.Next() {
		r, err := scanRound(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) AddPhoto(ctx context.Context, roundID, memberID int64, fileID string, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO photos(round_id, member_id, file_id, created_at) VALUES (?,?,?,?)`, roundID, memberID, fileID, now.Unix())
	return err
}

func (s *Store) Photos(ctx context.Context, roundID int64) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT file_id FROM photos WHERE round_id=? ORDER BY id`, roundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var f string
		rows.Scan(&f)
		out = append(out, f)
	}
	return out, rows.Err()
}

// ---------- jobs ----------

type Job struct {
	ID       int64
	Kind     string
	RunAt    time.Time
	Payload  json.RawMessage
	Attempts int
}

// Enqueue is idempotent by dedupeKey: the same reminder is never queued twice.
func (s *Store) Enqueue(ctx context.Context, kind string, runAt time.Time, payload any, dedupeKey string) error {
	b, _ := json.Marshal(payload)
	var dk any
	if dedupeKey != "" {
		dk = dedupeKey
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO jobs(kind, run_at, payload, dedupe_key) VALUES (?,?,?,?) ON CONFLICT(dedupe_key) DO NOTHING`,
		kind, runAt.Unix(), string(b), dk)
	return err
}

// Claim takes due jobs, pushing their run_at forward so a crash retries them later instead of twice now.
func (s *Store) Claim(ctx context.Context, now time.Time, limit int) ([]Job, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, kind, run_at, payload, attempts FROM jobs WHERE done_at IS NULL AND run_at<=? AND attempts<5 ORDER BY run_at LIMIT ?`, now.Unix(), limit)
	if err != nil {
		return nil, err
	}
	var out []Job
	for rows.Next() {
		var j Job
		var run int64
		var p string
		if err := rows.Scan(&j.ID, &j.Kind, &run, &p, &j.Attempts); err != nil {
			rows.Close()
			return nil, err
		}
		j.RunAt, j.Payload = time.Unix(run, 0), json.RawMessage(p)
		out = append(out, j)
	}
	rows.Close()
	for _, j := range out {
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET attempts=attempts+1, run_at=? WHERE id=?`, now.Add(2*time.Minute).Unix(), j.ID); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit()
}

func (s *Store) JobDone(ctx context.Context, id int64, now time.Time, errText string) error {
	if errText != "" {
		_, err := s.DB.ExecContext(ctx, `UPDATE jobs SET last_error=? WHERE id=?`, errText, id)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE jobs SET done_at=? WHERE id=?`, now.Unix(), id)
	return err
}

// LateJobs counts jobs due more than d ago that are still open (for the health check and alerts).
func (s *Store) LateJobs(ctx context.Context, now time.Time, d time.Duration) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE done_at IS NULL AND attempts<5 AND run_at<?`, now.Add(-d).Unix()).Scan(&n)
	return n, err
}

// ---------- telegram updates ----------

// FirstSeen records an update id and reports whether it is new (Telegram retries webhooks).
func (s *Store) FirstSeen(ctx context.Context, id int64, now time.Time) (bool, error) {
	r, err := s.DB.ExecContext(ctx, `INSERT INTO tg_updates(update_id, seen_at) VALUES (?,?) ON CONFLICT DO NOTHING`, id, now.Unix())
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n == 1, nil // true: first time we see this update
}

func (s *Store) LogAI(ctx context.Context, task, provider, in, out string, ok bool, ms int64, now time.Time) {
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO ai_log(task, provider, input, output, ok, ms, created_at) VALUES (?,?,?,?,?,?,?)`,
		task, provider, in, out, b2i(ok), ms, now.Unix())
}

// DeleteSpace removes every row of a couple (both asked for it).
func (s *Store) DeleteSpace(ctx context.Context, spaceID int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM photos WHERE round_id IN (SELECT id FROM rounds WHERE space_id=?)`,
		`DELETE FROM rounds WHERE space_id=?`, `DELETE FROM ideas WHERE space_id=?`, `DELETE FROM invites WHERE space_id=?`,
		`DELETE FROM members WHERE space_id=?`, `DELETE FROM spaces WHERE id=?`,
	} {
		if _, err := tx.ExecContext(ctx, q, spaceID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LogScout records an agent run (for the admin view and to not repeat itself).
func (s *Store) LogScout(ctx context.Context, spaceID int64, picks, prices int, note string, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO scout_runs(space_id, picks, prices, note, created_at) VALUES (?,?,?,?,?)`, spaceID, picks, prices, note, now.Unix())
	return err
}

// PlannedIdeas: ideas already agreed for a coming meeting — not offered again as «where to go».
func (s *Store) PlannedIdeas(ctx context.Context, spaceID int64) map[int64]bool {
	out := map[int64]bool{}
	rows, err := s.DB.QueryContext(ctx, `SELECT body FROM rounds WHERE space_id=? AND state IN ('planned','confirm')`, spaceID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		if r, err := scanRound(rows); err == nil && r.Chosen != 0 {
			out[r.Chosen] = true
		}
	}
	return out
}
