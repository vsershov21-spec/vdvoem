package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

func hashKey(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func (s *Store) MemberByID(ctx context.Context, id int64) (Member, error) {
	return scanMember(s.DB.QueryRowContext(ctx, `SELECT `+memberCols+` FROM members WHERE id=?`, id))
}

func (s *Store) SetMemberPrefs(ctx context.Context, id int64, biometric bool, theme string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE members SET biometric_lock=?, theme=? WHERE id=?`, b2i(biometric), theme, id)
	return err
}

// ---------- sessions ----------

type Session struct {
	ID        string // hash, safe to show as an id
	MemberID  int64
	Method    string
	UA        string
	CreatedAt time.Time
	SeenAt    time.Time
}

func (s *Store) CreateSession(ctx context.Context, token string, memberID int64, method, ua string, ttl time.Duration, now time.Time) error {
	if len(ua) > 200 {
		ua = ua[:200]
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions(id_hash, member_id, method, ua, created_at, seen_at, expires_at) VALUES (?,?,?,?,?,?,?)`,
		hashKey(token), memberID, method, ua, now.Unix(), now.Unix(), now.Add(ttl).Unix())
	return err
}

// SessionMember resolves a cookie token; it refreshes seen_at at most once a minute.
func (s *Store) SessionMember(ctx context.Context, token string, now time.Time) (int64, error) {
	var id, seen int64
	err := s.DB.QueryRowContext(ctx, `SELECT member_id, seen_at FROM sessions WHERE id_hash=? AND expires_at > ?`, hashKey(token), now.Unix()).Scan(&id, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err == nil && now.Unix()-seen > 60 {
		_, _ = s.DB.ExecContext(ctx, `UPDATE sessions SET seen_at=? WHERE id_hash=?`, now.Unix(), hashKey(token))
	}
	return id, err
}

func (s *Store) Sessions(ctx context.Context, memberID int64, now time.Time) ([]Session, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id_hash, member_id, method, ua, created_at, seen_at FROM sessions WHERE member_id=? AND expires_at > ? ORDER BY seen_at DESC`, memberID, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var x Session
		var c, sn int64
		if err := rows.Scan(&x.ID, &x.MemberID, &x.Method, &x.UA, &c, &sn); err != nil {
			return nil, err
		}
		x.CreatedAt, x.SeenAt = time.Unix(c, 0), time.Unix(sn, 0)
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash=?`, hashKey(token))
	return err
}

func (s *Store) DeleteSessionByID(ctx context.Context, memberID int64, idHash string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash=? AND member_id=?`, idHash, memberID)
	return err
}

func (s *Store) DeleteSessions(ctx context.Context, memberID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE member_id=?`, memberID)
	return err
}

// ---------- login through the bot ----------

func (s *Store) CreateLoginCode(ctx context.Context, code string, ttl time.Duration, now time.Time) error {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM login_codes WHERE expires_at < ?`, now.Add(-time.Hour).Unix())
	_, err := s.DB.ExecContext(ctx, `INSERT INTO login_codes(code_hash, created_at, expires_at) VALUES (?,?,?)`, hashKey(code), now.Unix(), now.Add(ttl).Unix())
	return err
}

// ConfirmLoginCode is called from the bot by the person who pressed «Да, это я».
func (s *Store) ConfirmLoginCode(ctx context.Context, code string, memberID int64, now time.Time) error {
	r, err := s.DB.ExecContext(ctx, `UPDATE login_codes SET member_id=? WHERE code_hash=? AND member_id IS NULL AND used_at IS NULL AND expires_at > ?`,
		memberID, hashKey(code), now.Unix())
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TakeLoginCode returns the confirmed member once; ErrNotFound while unconfirmed or after use/expiry.
func (s *Store) TakeLoginCode(ctx context.Context, code string, now time.Time) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `UPDATE login_codes SET used_at=? WHERE code_hash=? AND member_id IS NOT NULL AND used_at IS NULL AND expires_at > ? RETURNING member_id`,
		now.Unix(), hashKey(code), now.Unix()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

func (s *Store) LoginCodeAlive(ctx context.Context, code string, now time.Time) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM login_codes WHERE code_hash=? AND used_at IS NULL AND expires_at > ?`, hashKey(code), now.Unix()).Scan(&n)
	return n == 1
}

// ---------- passwords ----------

type Password struct {
	MemberID    int64
	Login       string
	Hash, Salt  []byte
	Iters       int
	Fails       int
	LockedUntil time.Time
}

var ErrLoginTaken = errors.New("store: login taken")

func (s *Store) SetPassword(ctx context.Context, p Password, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO passwords(member_id, login, hash, salt, iters, updated_at) VALUES (?,?,?,?,?,?)
		ON CONFLICT(member_id) DO UPDATE SET login=excluded.login, hash=excluded.hash, salt=excluded.salt, iters=excluded.iters, fails=0, locked_until=0, updated_at=excluded.updated_at`,
		p.MemberID, p.Login, p.Hash, p.Salt, p.Iters, now.Unix())
	if err != nil && (contains(err.Error(), "UNIQUE") || contains(err.Error(), "constraint")) {
		return ErrLoginTaken
	}
	return err
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func (s *Store) PasswordByLogin(ctx context.Context, login string) (Password, error) {
	return scanPassword(s.DB.QueryRowContext(ctx, `SELECT member_id, login, hash, salt, iters, fails, locked_until FROM passwords WHERE login=?`, login))
}

func (s *Store) PasswordOf(ctx context.Context, memberID int64) (Password, error) {
	return scanPassword(s.DB.QueryRowContext(ctx, `SELECT member_id, login, hash, salt, iters, fails, locked_until FROM passwords WHERE member_id=?`, memberID))
}

func scanPassword(r *sql.Row) (Password, error) {
	var p Password
	var lu int64
	err := r.Scan(&p.MemberID, &p.Login, &p.Hash, &p.Salt, &p.Iters, &p.Fails, &lu)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	p.LockedUntil = time.Unix(lu, 0)
	return p, err
}

// PasswordFail counts a wrong attempt; after `max` the login is locked for `lock`.
func (s *Store) PasswordFail(ctx context.Context, memberID int64, max int, lock time.Duration, now time.Time) error {
	// right-hand sides see the old row, so both CASEs test the same fails+1
	_, err := s.DB.ExecContext(ctx, `UPDATE passwords SET
		fails = CASE WHEN fails + 1 >= ? THEN 0 ELSE fails + 1 END,
		locked_until = CASE WHEN fails + 1 >= ? THEN ? ELSE locked_until END
		WHERE member_id=?`, max, max, now.Add(lock).Unix(), memberID)
	return err
}

func (s *Store) PasswordOK(ctx context.Context, memberID int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE passwords SET fails=0, locked_until=0 WHERE member_id=?`, memberID)
	return err
}

func (s *Store) DeletePassword(ctx context.Context, memberID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM passwords WHERE member_id=?`, memberID)
	return err
}

// ---------- passkeys ----------

type Passkey struct {
	ID        string
	MemberID  int64
	PublicKey []byte
	Alg       int
	SignCount uint32
	Name      string
	CreatedAt time.Time
	UsedAt    *time.Time
}

func (s *Store) AddPasskey(ctx context.Context, p Passkey, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO passkeys(id, member_id, public_key, alg, sign_count, name, created_at) VALUES (?,?,?,?,?,?,?)`,
		p.ID, p.MemberID, p.PublicKey, p.Alg, p.SignCount, p.Name, now.Unix())
	return err
}

func (s *Store) Passkey(ctx context.Context, id string) (Passkey, error) {
	var p Passkey
	var c int64
	var u sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT id, member_id, public_key, alg, sign_count, name, created_at, used_at FROM passkeys WHERE id=?`, id).
		Scan(&p.ID, &p.MemberID, &p.PublicKey, &p.Alg, &p.SignCount, &p.Name, &c, &u)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	p.CreatedAt = time.Unix(c, 0)
	return p, err
}

func (s *Store) Passkeys(ctx context.Context, memberID int64) ([]Passkey, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, member_id, public_key, alg, sign_count, name, created_at, used_at FROM passkeys WHERE member_id=? ORDER BY created_at`, memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Passkey
	for rows.Next() {
		var p Passkey
		var c int64
		var u sql.NullInt64
		if err := rows.Scan(&p.ID, &p.MemberID, &p.PublicKey, &p.Alg, &p.SignCount, &p.Name, &c, &u); err != nil {
			return nil, err
		}
		p.CreatedAt = time.Unix(c, 0)
		if u.Valid {
			t := time.Unix(u.Int64, 0)
			p.UsedAt = &t
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) TouchPasskey(ctx context.Context, id string, count uint32, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE passkeys SET sign_count=?, used_at=? WHERE id=?`, count, now.Unix(), id)
	return err
}

func (s *Store) DeletePasskey(ctx context.Context, memberID int64, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM passkeys WHERE id=? AND member_id=?`, id, memberID)
	return err
}

func (s *Store) SaveChallenge(ctx context.Context, ch string, memberID int64, kind string, ttl time.Duration, now time.Time) error {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM webauthn_challenges WHERE expires_at < ?`, now.Unix())
	var m any
	if memberID != 0 {
		m = memberID
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO webauthn_challenges(challenge, member_id, kind, expires_at) VALUES (?,?,?,?)`, ch, m, kind, now.Add(ttl).Unix())
	return err
}

// TakeChallenge consumes a challenge once.
func (s *Store) TakeChallenge(ctx context.Context, ch, kind string, now time.Time) (int64, error) {
	var m sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `DELETE FROM webauthn_challenges WHERE challenge=? AND kind=? AND expires_at > ? RETURNING member_id`, ch, kind, now.Unix()).Scan(&m)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return m.Int64, err
}
func HashToken(s string) string { return hashKey(s) }
