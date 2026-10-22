package token

import (
	"strings"
	"testing"
	"time"
)

func TestRoundTripAndRejections(t *testing.T) {
	secret := []byte("s3cret")
	now := time.Unix(1_000_000, 0)
	tok := Sign(secret, Claims{Session: "vm-1", Index: 7, Expires: now.Add(time.Minute).Unix(), Kind: "cdp"})

	c, err := Verify(secret, tok, now)
	if err != nil || c.Index != 7 || c.Session != "vm-1" {
		t.Fatalf("valid token rejected: %v %+v", err, c)
	}
	if _, err := Verify([]byte("other"), tok, now); err == nil {
		t.Error("wrong secret accepted")
	}
	if _, err := Verify(secret, tok, now.Add(2*time.Minute)); err == nil {
		t.Error("expired token accepted")
	}
	// Change the claims (index 7 -> 8) but keep the signature.
	body, sig, _ := strings.Cut(tok, ".")
	forged := Sign([]byte("attacker"), Claims{Session: "vm-1", Index: 8, Expires: now.Add(time.Minute).Unix()})
	fb, _, _ := strings.Cut(forged, ".")
	if _, err := Verify(secret, fb+"."+sig, now); err == nil {
		t.Error("tampered claims accepted")
	}
	if _, err := Verify(secret, body, now); err == nil {
		t.Error("token without signature accepted")
	}
}
