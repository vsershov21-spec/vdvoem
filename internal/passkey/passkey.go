// Package passkey verifies WebAuthn registrations and assertions (Face ID / Touch ID / passkeys) with the
// standard library only. Registration relies on the browser's AuthenticatorAttestationResponse.getPublicKey()
// (SPKI DER) and getAuthenticatorData(), so no CBOR decoding is needed; attestation is "none".
package passkey

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
)

const (
	AlgES256 = -7
	AlgRS256 = -257
)

var (
	ErrClientData = errors.New("passkey: client data mismatch")
	ErrRP         = errors.New("passkey: wrong relying party")
	ErrFlags      = errors.New("passkey: user not present or not verified")
	ErrSignature  = errors.New("passkey: bad signature")
	ErrKey        = errors.New("passkey: unsupported key")
	ErrCounter    = errors.New("passkey: sign counter went back (cloned authenticator?)")
)

// RP is the relying party: the site's host (rpId) and its exact origin.
type RP struct {
	ID     string // vdvoem.vershov.com
	Origin string // https://vdvoem.vershov.com
}

func B64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func Unb64(s string) ([]byte, error) {
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

type clientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin"`
}

func (rp RP) checkClient(raw []byte, typ, challenge string) error {
	var c clientData
	if err := json.Unmarshal(raw, &c); err != nil {
		return ErrClientData
	}
	if c.Type != typ || c.Challenge != challenge || c.Origin != rp.Origin || c.CrossOrigin {
		return fmt.Errorf("%w: type=%s origin=%s", ErrClientData, c.Type, c.Origin)
	}
	return nil
}

type authData struct {
	flags byte
	count uint32
}

// parseAuthData checks rpIdHash, UP and UV flags.
func (rp RP) parseAuthData(ad []byte, requireUV bool) (authData, error) {
	if len(ad) < 37 {
		return authData{}, ErrRP
	}
	want := sha256.Sum256([]byte(rp.ID))
	if !bytes.Equal(ad[:32], want[:]) {
		return authData{}, ErrRP
	}
	f := ad[32]
	if f&0x01 == 0 || (requireUV && f&0x04 == 0) {
		return authData{}, ErrFlags
	}
	return authData{flags: f, count: binary.BigEndian.Uint32(ad[33:37])}, nil
}

// Registration is what the browser sends after navigator.credentials.create().
type Registration struct {
	ID                string `json:"id"`                 // base64url credential id
	ClientDataJSON    string `json:"client_data_json"`   // base64url
	AuthenticatorData string `json:"authenticator_data"` // base64url, from getAuthenticatorData()
	PublicKey         string `json:"public_key"`         // base64url SPKI DER, from getPublicKey()
	Alg               int    `json:"alg"`                // from getPublicKeyAlgorithm()
}

type Credential struct {
	ID        string
	PublicKey []byte
	Alg       int
	SignCount uint32
}

func (rp RP) VerifyRegistration(r Registration, challenge string) (Credential, error) {
	cd, err := Unb64(r.ClientDataJSON)
	if err != nil {
		return Credential{}, ErrClientData
	}
	if err := rp.checkClient(cd, "webauthn.create", challenge); err != nil {
		return Credential{}, err
	}
	ad, err := Unb64(r.AuthenticatorData)
	if err != nil {
		return Credential{}, ErrRP
	}
	a, err := rp.parseAuthData(ad, true)
	if err != nil {
		return Credential{}, err
	}
	pk, err := Unb64(r.PublicKey)
	if err != nil {
		return Credential{}, ErrKey
	}
	if _, err := parseKey(pk, r.Alg); err != nil {
		return Credential{}, err
	}
	if _, err := Unb64(r.ID); err != nil || r.ID == "" {
		return Credential{}, ErrKey
	}
	return Credential{ID: r.ID, PublicKey: pk, Alg: r.Alg, SignCount: a.count}, nil
}

// Assertion is what the browser sends after navigator.credentials.get().
type Assertion struct {
	ID                string `json:"id"`
	ClientDataJSON    string `json:"client_data_json"`
	AuthenticatorData string `json:"authenticator_data"`
	Signature         string `json:"signature"`
}

// VerifyAssertion returns the new sign counter.
func (rp RP) VerifyAssertion(as Assertion, challenge string, cred Credential) (uint32, error) {
	cd, err := Unb64(as.ClientDataJSON)
	if err != nil {
		return 0, ErrClientData
	}
	if err := rp.checkClient(cd, "webauthn.get", challenge); err != nil {
		return 0, err
	}
	ad, err := Unb64(as.AuthenticatorData)
	if err != nil {
		return 0, ErrRP
	}
	a, err := rp.parseAuthData(ad, true)
	if err != nil {
		return 0, err
	}
	sig, err := Unb64(as.Signature)
	if err != nil {
		return 0, ErrSignature
	}
	key, err := parseKey(cred.PublicKey, cred.Alg)
	if err != nil {
		return 0, err
	}
	h := sha256.Sum256(cd)
	msg := append(append([]byte{}, ad...), h[:]...)
	digest := sha256.Sum256(msg)
	switch k := key.(type) {
	case *ecdsa.PublicKey:
		if !ecdsa.VerifyASN1(k, digest[:], sig) {
			return 0, ErrSignature
		}
	case *rsa.PublicKey:
		if rsa.VerifyPKCS1v15(k, crypto.SHA256, digest[:], sig) != nil {
			return 0, ErrSignature
		}
	}
	// counters are optional (0 forever on many platforms); when used they must grow
	if a.count != 0 || cred.SignCount != 0 {
		if a.count <= cred.SignCount {
			return 0, ErrCounter
		}
	}
	return a.count, nil
}

func parseKey(spki []byte, alg int) (any, error) {
	k, err := x509.ParsePKIXPublicKey(spki)
	if err != nil {
		return nil, ErrKey
	}
	switch kk := k.(type) {
	case *ecdsa.PublicKey:
		if alg != AlgES256 || kk.Curve.Params().BitSize != 256 {
			return nil, ErrKey
		}
	case *rsa.PublicKey:
		if alg != AlgRS256 || kk.N.Cmp(big.NewInt(1).Lsh(big.NewInt(1), 2047)) < 0 {
			return nil, ErrKey
		}
	default:
		return nil, ErrKey
	}
	return k, nil
}
