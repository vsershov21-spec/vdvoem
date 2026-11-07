package tgauth

import (
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	tok := "123:test-token"
	now := time.Unix(1_800_000_000, 0)
	v := url.Values{}
	v.Set("auth_date", strconv.FormatInt(now.Unix(), 10))
	v.Set("user", `{"id":42,"first_name":"Настя"}`)
	raw := Sign(tok, v)
	d, err := Validate(raw, tok, 24*time.Hour, now.Add(time.Hour))
	if err != nil || d.User.ID != 42 || d.User.FirstName != "Настя" {
		t.Fatal(err, d)
	}
	if _, err := Validate(raw, "other:token", 0, now); err != ErrBadHash {
		t.Fatal("wrong token accepted")
	}
	if _, err := Validate(raw, tok, 24*time.Hour, now.Add(25*time.Hour)); err != ErrExpired {
		t.Fatal("old initData accepted")
	}
	tampered, _ := url.ParseQuery(raw)
	tampered.Set("user", `{"id":43}`)
	if _, err := Validate(tampered.Encode(), tok, 0, now); err != ErrBadHash {
		t.Fatal("tampered user accepted")
	}
}
