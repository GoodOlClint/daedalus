package brokerauth_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zakros-hq/zakros/pkg/audit"
	"github.com/zakros-hq/zakros/pkg/brokerauth"
	"github.com/zakros-hq/zakros/pkg/jwt"
)

// claimsFor builds a JWT with the given audience and scope-set, signed
// by the supplied private key. Helper that keeps each test small.
func claimsFor(t *testing.T, priv ed25519.PrivateKey, broker, scope string) string {
	t.Helper()
	now := time.Now().UTC()
	c := jwt.Claims{
		Subject:  "pod:test:run-1",
		Issuer:   "minos",
		Audience: []string{broker},
		IssuedAt: now,
		Expires:  now.Add(time.Hour),
		JTI:      time.Now().Format(time.RFC3339Nano),
		McpScopes: map[string][]string{
			broker: {scope},
		},
	}
	tok, err := jwt.Sign(priv, c)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return tok
}

func newVerifier(t *testing.T, broker string) (*brokerauth.Verifier, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	return &brokerauth.Verifier{
		Broker:    broker,
		PublicKey: pub,
		Replay:    brokerauth.NewMemReplayStore(),
		Audit:     audit.NewWriterEmitter("test", testWriter{}),
	}, priv
}

type testWriter struct{}

func (testWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestVerifierAllowsValidToken(t *testing.T) {
	v, priv := newVerifier(t, "github")
	tok := claimsFor(t, priv, "github", "pr.create")

	handler := v.Require("pr.create", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := brokerauth.ClaimsFromContext(r.Context())
		if c == nil || c.Subject != "pod:test:run-1" {
			t.Errorf("claims missing or wrong: %+v", c)
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("POST", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestVerifierRejectsMissingBearer(t *testing.T) {
	v, _ := newVerifier(t, "github")
	handler := v.Require("pr.create", noopHandler())
	req := httptest.NewRequest("POST", "/x", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestVerifierRejectsAudienceMismatch(t *testing.T) {
	v, priv := newVerifier(t, "github")
	tok := claimsFor(t, priv, "mnemosyne", "memory.lookup") // wrong audience

	handler := v.Require("pr.create", noopHandler())
	req := httptest.NewRequest("POST", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for audience mismatch, got %d", rec.Code)
	}
}

func TestVerifierRejectsScopeMissing(t *testing.T) {
	v, priv := newVerifier(t, "github")
	tok := claimsFor(t, priv, "github", "pr.comment") // wrong scope

	handler := v.Require("pr.create", noopHandler())
	req := httptest.NewRequest("POST", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for scope mismatch, got %d", rec.Code)
	}
}

func TestVerifierRejectsExpired(t *testing.T) {
	v, _ := newVerifier(t, "github")
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	v.PublicKey = pub
	now := time.Now().UTC()
	c := jwt.Claims{
		Subject:   "pod:test:run-1",
		Issuer:    "minos",
		Audience:  []string{"github"},
		IssuedAt:  now.Add(-2 * time.Hour),
		Expires:   now.Add(-time.Hour),
		JTI:       "x",
		McpScopes: map[string][]string{"github": {"pr.create"}},
	}
	tok, _ := jwt.Sign(priv, c)

	handler := v.Require("pr.create", noopHandler())
	req := httptest.NewRequest("POST", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	// jwt.Verify itself rejects expired tokens — Verifier returns 401.
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for expired token, got %d", rec.Code)
	}
}

func TestVerifierRejectsReplay(t *testing.T) {
	v, priv := newVerifier(t, "github")
	tok := claimsFor(t, priv, "github", "pr.create")

	handler := v.Require("pr.create", noopHandler())

	// First request wins.
	req := httptest.NewRequest("POST", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first call should pass, got %d", rec.Code)
	}

	// Replay refused.
	req2 := httptest.NewRequest("POST", "/x", nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on replay, got %d", rec2.Code)
	}
}

func TestVerifierAllowsReplayWhenStoreIsNop(t *testing.T) {
	v, priv := newVerifier(t, "github")
	v.Replay = brokerauth.NopReplayStore{}
	tok := claimsFor(t, priv, "github", "pr.create")

	handler := v.Require("pr.create", noopHandler())
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/x", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("call %d: expected 200, got %d", i, rec.Code)
		}
	}
}

func TestVerifierEmptyScopeSkipsScopeCheck(t *testing.T) {
	// scope == "" means: any-scope-OK on this endpoint. Used by the
	// future github-broker /github/installation-token route which only
	// requires audience match, not a specific operation scope.
	v, priv := newVerifier(t, "github")
	tok := claimsFor(t, priv, "github", "anything")

	handler := v.Require("", noopHandler())
	req := httptest.NewRequest("POST", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func noopHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// _ silences "declared but not used" if a future test needs it.
var _ = context.Background
