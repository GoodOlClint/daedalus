package jwt_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/zakros-hq/zakros/pkg/jwt"
)

func sampleClaims() jwt.Claims {
	now := time.Now().UTC()
	return jwt.Claims{
		Subject:  "pod:task-123:run-1",
		Issuer:   "minos",
		Audience: []string{"github", "mnemosyne"},
		IssuedAt: now,
		Expires:  now.Add(2 * time.Hour),
		JTI:      "token-1",
		McpScopes: map[string][]string{
			"github":    {"pr.create", "pr.update"},
			"mnemosyne": {"memory.lookup"},
		},
	}
}

// freshKeypair returns a fresh Ed25519 keypair for one test.
func freshKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	return pub, priv
}

func TestSignAndVerifyRoundTrip(t *testing.T) {
	pub, priv := freshKeypair(t)
	c := sampleClaims()
	tok, err := jwt.Sign(priv, c)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, err := jwt.Verify(pub, tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Subject != c.Subject || got.Issuer != c.Issuer {
		t.Errorf("claim mismatch: %+v vs %+v", got, c)
	}
	if !got.HasScope("github", "pr.create") {
		t.Errorf("expected github:pr.create scope to survive round trip")
	}
	if got.HasScope("github", "nonexistent") {
		t.Errorf("unexpected scope present")
	}
	if !got.HasAudience("github") {
		t.Errorf("expected github audience to survive round trip")
	}
	if got.HasAudience("nonexistent") {
		t.Errorf("unexpected audience present")
	}
}

func TestVerifyWrongKey(t *testing.T) {
	_, signingKey := freshKeypair(t)
	otherPub, _ := freshKeypair(t)
	c := sampleClaims()
	tok, err := jwt.Sign(signingKey, c)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := jwt.Verify(otherPub, tok); !errors.Is(err, jwt.ErrInvalidBearer) {
		t.Errorf("expected ErrInvalidBearer, got %v", err)
	}
}

func TestVerifyExpired(t *testing.T) {
	pub, priv := freshKeypair(t)
	c := sampleClaims()
	c.IssuedAt = time.Now().Add(-3 * time.Hour)
	c.Expires = time.Now().Add(-1 * time.Hour)
	tok, err := jwt.Sign(priv, c)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := jwt.Verify(pub, tok); !errors.Is(err, jwt.ErrInvalidBearer) {
		t.Errorf("expected ErrInvalidBearer for expired token, got %v", err)
	}
}

func TestSignRequiresKey(t *testing.T) {
	if _, err := jwt.Sign(nil, sampleClaims()); !errors.Is(err, jwt.ErrInvalidBearer) {
		t.Errorf("expected ErrInvalidBearer for empty key, got %v", err)
	}
}

func TestKeypairRoundTripPEM(t *testing.T) {
	pub, priv, err := jwt.GenerateKeypair()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	privPEM, err := jwt.MarshalPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal priv: %v", err)
	}
	pubPEM, err := jwt.MarshalPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal pub: %v", err)
	}
	priv2, err := jwt.ParsePrivateKey(privPEM)
	if err != nil {
		t.Fatalf("parse priv: %v", err)
	}
	pub2, err := jwt.ParsePublicKey(pubPEM)
	if err != nil {
		t.Fatalf("parse pub: %v", err)
	}
	tok, err := jwt.Sign(priv2, sampleClaims())
	if err != nil {
		t.Fatalf("sign with parsed priv: %v", err)
	}
	if _, err := jwt.Verify(pub2, tok); err != nil {
		t.Fatalf("verify with parsed pub: %v", err)
	}
}

func TestParsePublicKeyRejectsGarbage(t *testing.T) {
	if _, err := jwt.ParsePublicKey([]byte("not a pem block")); !errors.Is(err, jwt.ErrKeyMaterial) {
		t.Errorf("expected ErrKeyMaterial, got %v", err)
	}
}
