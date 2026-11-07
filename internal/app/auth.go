package app

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"vdvoem/internal/passkey"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

const (
	SessionTTL   = 30 * 24 * time.Hour
	LoginCodeTTL = 5 * time.Minute
	pbkdfIters   = 600_000
	maxPassFails = 5
	passLock     = 15 * time.Minute
	challengeTTL = 5 * time.Minute
)

var (
	ErrBadLogin   = errors.New("bad_login")
	ErrLocked     = errors.New("locked")
	ErrWeakPass   = errors.New("weak_password")
	ErrLoginTaken = errors.New("login_taken")
	ErrBadLoginID = errors.New("bad_login_name")
)

func Token(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// CoupleByMember is the entry point for cookie sessions.
func (a *App) CoupleByMember(ctx context.Context, memberID int64) (Couple, error) {
	m, err := a.S.MemberByID(ctx, memberID)
	if errors.Is(err, store.ErrNotFound) {
		return Couple{}, ErrNoSpace
	}
	if err != nil {
		return Couple{}, err
	}
	return a.coupleOf(ctx, m)
}

// NewSession issues a cookie token for a member.
func (a *App) NewSession(ctx context.Context, memberID int64, method, ua string) (string, error) {
	t := Token(32)
	return t, a.S.CreateSession(ctx, t, memberID, method, ua, SessionTTL, a.now())
}

// ---------- through the bot ----------

// LoginCode creates a code the web page shows as a t.me deep link.
func (a *App) LoginCode(ctx context.Context) (code, link string, err error) {
	code = strings.ToUpper(Token(9))
	if err := a.S.CreateLoginCode(ctx, code, LoginCodeTTL, a.now()); err != nil {
		return "", "", err
	}
	return code, fmt.Sprintf("https://t.me/%s?start=login_%s", a.BotName, code), nil
}

// AskLogin is what the bot shows after /start login_<code>.
func (a *App) AskLogin(ctx context.Context, chatID int64, code string) {
	if !a.S.LoginCodeAlive(ctx, code, a.now()) {
		a.send(ctx, chatID, "Ссылка для входа устарела. Открой страницу входа ещё раз.", nil)
		return
	}
	a.send(ctx, chatID, "<b>Войти во «Вдвоём» в браузере?</b>\nЕсли это не ты — просто ничего не нажимай.",
		tg.Keyboard(tg.Row(tg.CB("Да, это я", "lg:"+code), tg.CB("Нет", "lgx"))))
}

// AskUnlock is the way past the Face ID lock when the sensor fails: the bot asks in the member's own chat.
func (a *App) AskUnlock(ctx context.Context, c Couple) (string, error) {
	code := strings.ToUpper(Token(9))
	if err := a.S.CreateLoginCode(ctx, code, LoginCodeTTL, a.now()); err != nil {
		return "", err
	}
	if c.Me.ChatID == 0 {
		return "", ErrNoChat
	}
	a.send(ctx, c.Me.ChatID, "<b>Открыть «Вдвоём» без Face ID?</b>\nЕсли это не ты — просто ничего не нажимай.",
		tg.Keyboard(tg.Row(tg.CB("Да, это я", "lg:"+code), tg.CB("Нет", "lgx"))))
	return code, nil
}

var ErrNoChat = errors.New("no_chat")

func (a *App) ConfirmLogin(ctx context.Context, c Couple, code string) error {
	return a.S.ConfirmLoginCode(ctx, code, c.Me.ID, a.now())
}

// PollLogin returns the member once the code was confirmed in the bot.
func (a *App) PollLogin(ctx context.Context, code string) (int64, bool, error) {
	id, err := a.S.TakeLoginCode(ctx, code, a.now())
	if errors.Is(err, store.ErrNotFound) {
		return 0, a.S.LoginCodeAlive(ctx, code, a.now()), nil
	}
	return id, true, err
}

// ---------- login & password ----------

var loginRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{3,32}$`)

func hashPass(pass string, salt []byte, iters int) []byte {
	k, _ := pbkdf2.Key(sha256.New, pass, salt, iters, 32)
	return k
}

func (a *App) SetPassword(ctx context.Context, c Couple, login, pass string) error {
	login = strings.TrimSpace(login)
	if !loginRe.MatchString(login) {
		return ErrBadLoginID
	}
	if utf8.RuneCountInString(pass) < 8 || strings.EqualFold(pass, login) {
		return ErrWeakPass
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	err := a.S.SetPassword(ctx, store.Password{MemberID: c.Me.ID, Login: login, Hash: hashPass(pass, salt, pbkdfIters), Salt: salt, Iters: pbkdfIters}, a.now())
	if errors.Is(err, store.ErrLoginTaken) {
		return ErrLoginTaken
	}
	return err
}

// dummy work for unknown logins, so timing does not reveal which logins exist
var dummySalt = []byte("vdvoem-dummy-salt")

func (a *App) PasswordLogin(ctx context.Context, login, pass string) (int64, error) {
	p, err := a.S.PasswordByLogin(ctx, strings.TrimSpace(login))
	if errors.Is(err, store.ErrNotFound) {
		hashPass(pass, dummySalt, pbkdfIters)
		return 0, ErrBadLogin
	}
	if err != nil {
		return 0, err
	}
	if p.LockedUntil.After(a.now()) {
		return 0, ErrLocked
	}
	if subtle.ConstantTimeCompare(hashPass(pass, p.Salt, p.Iters), p.Hash) != 1 {
		_ = a.S.PasswordFail(ctx, p.MemberID, maxPassFails, passLock, a.now())
		return 0, ErrBadLogin
	}
	_ = a.S.PasswordOK(ctx, p.MemberID)
	return p.MemberID, nil
}

// ---------- passkeys ----------

func (a *App) RP() passkey.RP {
	host := strings.TrimPrefix(strings.TrimPrefix(a.PublicURL, "https://"), "http://")
	if i := strings.IndexAny(host, ":/"); i >= 0 {
		host = host[:i]
	}
	return passkey.RP{ID: host, Origin: strings.TrimRight(a.PublicURL, "/")}
}

func (a *App) PasskeyChallenge(ctx context.Context, memberID int64, kind string) (string, error) {
	ch := passkey.B64([]byte(Token(16)))
	return ch, a.S.SaveChallenge(ctx, ch, memberID, kind, challengeTTL, a.now())
}

func (a *App) AddPasskey(ctx context.Context, c Couple, r passkey.Registration, challenge, name string) error {
	who, err := a.S.TakeChallenge(ctx, challenge, "register", a.now())
	if err != nil || who != c.Me.ID {
		return passkey.ErrClientData
	}
	cred, err := a.RP().VerifyRegistration(r, challenge)
	if err != nil {
		return err
	}
	if n := []rune(strings.TrimSpace(name)); len(n) > 40 {
		name = string(n[:40])
	}
	if name == "" {
		name = "Face ID"
	}
	return a.S.AddPasskey(ctx, store.Passkey{ID: cred.ID, MemberID: c.Me.ID, PublicKey: cred.PublicKey, Alg: cred.Alg, SignCount: cred.SignCount, Name: name}, a.now())
}

func (a *App) PasskeyLogin(ctx context.Context, as passkey.Assertion, challenge string) (int64, error) {
	if _, err := a.S.TakeChallenge(ctx, challenge, "login", a.now()); err != nil {
		return 0, passkey.ErrClientData
	}
	k, err := a.S.Passkey(ctx, as.ID)
	if err != nil {
		return 0, ErrBadLogin
	}
	n, err := a.RP().VerifyAssertion(as, challenge, passkey.Credential{ID: k.ID, PublicKey: k.PublicKey, Alg: k.Alg, SignCount: k.SignCount})
	if err != nil {
		return 0, err
	}
	_ = a.S.TouchPasskey(ctx, k.ID, n, a.now())
	return k.MemberID, nil
}
