// Package token issues and checks the tokens that let a client reach one
// browser. A token is a small JSON claim plus an HMAC signature over it. The
// proxy checks the signature with the shared secret, so it needs no database
// and no call to anyone to decide whether to let a connection through.
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Claims say which VM the bearer may reach, until when, and for what.
type Claims struct {
	Session string `json:"s"`           // the VM / session id
	Index   int    `json:"n"`           // the VM's network index
	Expires int64  `json:"exp"`         // unix seconds
	Kind    string `json:"k,omitempty"` // "cdp" (full control) or "view" (live view)
}

var enc = base64.RawURLEncoding

// Sign returns claims.signature, both base64url, safe in a URL path.
func Sign(secret []byte, c Claims) string {
	body, _ := json.Marshal(c)
	b := enc.EncodeToString(body)
	return b + "." + enc.EncodeToString(mac(secret, b))
}

// Verify checks the signature and the expiry and returns the claims.
func Verify(secret []byte, tok string, now time.Time) (Claims, error) {
	var c Claims
	b, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return c, errors.New("malformed token")
	}
	got, err := enc.DecodeString(sig)
	if err != nil || !hmac.Equal(got, mac(secret, b)) { // constant-time comparison
		return c, errors.New("bad signature")
	}
	body, err := enc.DecodeString(b)
	if err != nil || json.Unmarshal(body, &c) != nil {
		return c, errors.New("malformed token")
	}
	if now.Unix() >= c.Expires {
		return c, errors.New("token expired")
	}
	return c, nil
}

func mac(secret []byte, s string) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(s))
	return h.Sum(nil)
}
