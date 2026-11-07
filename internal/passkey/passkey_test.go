package passkey

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"
)

func mkAuthData(rpID string, flags byte, count uint32) []byte {
	h := sha256.Sum256([]byte(rpID))
	ad := append(h[:], flags)
	c := make([]byte, 4)
	binary.BigEndian.PutUint32(c, count)
	return append(ad, c...)
}

func cd(typ, ch, origin string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": ch, "origin": origin})
	return b
}

func TestRoundTrip(t *testing.T) {
	rp := RP{ID: "vdvoem.vershov.com", Origin: "https://vdvoem.vershov.com"}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	spki, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	reg := Registration{ID: B64([]byte("cred-1")), ClientDataJSON: B64(cd("webauthn.create", "abc", rp.Origin)),
		AuthenticatorData: B64(mkAuthData(rp.ID, 0x45, 0)), PublicKey: B64(spki), Alg: AlgES256}
	cred, err := rp.VerifyRegistration(reg, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rp.VerifyRegistration(reg, "other"); !errors.Is(err, ErrClientData) {
		t.Fatal("challenge must match")
	}
	bad := reg
	bad.AuthenticatorData = B64(mkAuthData("evil.com", 0x45, 0))
	if _, err := rp.VerifyRegistration(bad, "abc"); !errors.Is(err, ErrRP) {
		t.Fatal("rp id must match")
	}
	noUV := reg
	noUV.AuthenticatorData = B64(mkAuthData(rp.ID, 0x01, 0))
	if _, err := rp.VerifyRegistration(noUV, "abc"); !errors.Is(err, ErrFlags) {
		t.Fatal("UV required")
	}

	sign := func(ch string, count uint32) Assertion {
		ad := mkAuthData(rp.ID, 0x05, count)
		c := cd("webauthn.get", ch, rp.Origin)
		h := sha256.Sum256(c)
		d := sha256.Sum256(append(append([]byte{}, ad...), h[:]...))
		sig, _ := ecdsa.SignASN1(rand.Reader, key, d[:])
		return Assertion{ID: cred.ID, ClientDataJSON: B64(c), AuthenticatorData: B64(ad), Signature: B64(sig)}
	}
	n, err := rp.VerifyAssertion(sign("xyz", 1), "xyz", cred)
	if err != nil || n != 1 {
		t.Fatal(err, n)
	}
	cred.SignCount = 1
	if _, err := rp.VerifyAssertion(sign("xyz", 1), "xyz", cred); !errors.Is(err, ErrCounter) {
		t.Fatal("counter must grow")
	}
	a := sign("xyz", 2)
	a.Signature = sign("xyz", 3).Signature
	if _, err := rp.VerifyAssertion(a, "xyz", cred); !errors.Is(err, ErrSignature) {
		t.Fatal("signature over other data must fail")
	}
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ospki, _ := x509.MarshalPKIXPublicKey(&other.PublicKey)
	if _, err := rp.VerifyAssertion(sign("q", 5), "q", Credential{PublicKey: ospki, Alg: AlgES256}); !errors.Is(err, ErrSignature) {
		t.Fatal("other key must fail")
	}
}
