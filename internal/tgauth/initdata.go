// Package tgauth validates Telegram Mini App initData (HMAC-SHA-256, key = HMAC("WebAppData", botToken)).
package tgauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	Language  string `json:"language_code"`
	PhotoURL  string `json:"photo_url"`
}

type Data struct {
	User       User
	AuthDate   time.Time
	StartParam string
	QueryID    string
}

var (
	ErrNoHash  = errors.New("initdata: no hash")
	ErrBadHash = errors.New("initdata: bad hash")
	ErrExpired = errors.New("initdata: expired")
	ErrNoUser  = errors.New("initdata: no user")
)

func secret(botToken string) []byte {
	m := hmac.New(sha256.New, []byte("WebAppData"))
	m.Write([]byte(botToken))
	return m.Sum(nil)
}

// Sign builds a valid initData string — used by tests and the local emulator.
func Sign(botToken string, values url.Values) string {
	values.Del("hash")
	h := hmac.New(sha256.New, secret(botToken))
	h.Write([]byte(checkString(values)))
	values.Set("hash", hex.EncodeToString(h.Sum(nil)))
	return values.Encode()
}

func checkString(v url.Values) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		if k != "hash" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + v.Get(k)
	}
	return strings.Join(parts, "\n")
}

// Validate checks the signature and age (maxAge 0 = no age check).
func Validate(raw, botToken string, maxAge time.Duration, now time.Time) (Data, error) {
	v, err := url.ParseQuery(raw)
	if err != nil {
		return Data{}, err
	}
	got := v.Get("hash")
	if got == "" {
		return Data{}, ErrNoHash
	}
	h := hmac.New(sha256.New, secret(botToken))
	h.Write([]byte(checkString(v)))
	want := hex.EncodeToString(h.Sum(nil))
	if !hmac.Equal([]byte(got), []byte(want)) {
		return Data{}, ErrBadHash
	}
	ad, _ := strconv.ParseInt(v.Get("auth_date"), 10, 64)
	d := Data{AuthDate: time.Unix(ad, 0), StartParam: v.Get("start_param"), QueryID: v.Get("query_id")}
	if maxAge > 0 && now.Sub(d.AuthDate) > maxAge {
		return Data{}, ErrExpired
	}
	if err := json.Unmarshal([]byte(v.Get("user")), &d.User); err != nil || d.User.ID == 0 {
		return Data{}, ErrNoUser
	}
	return d, nil
}
