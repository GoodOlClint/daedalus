// Package github verifies GitHub webhook requests per security.md §2.
// Phase 1 ingress is Cloudflare Tunnel → Cerberus-as-library-in-Minos;
// this verifier enforces HMAC-SHA256 over the raw body plus delivery-ID
// replay protection against a caller-supplied store.
package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrInvalidSignature indicates the X-Hub-Signature-256 header did not
// match HMAC-SHA256(secret, body).
var ErrInvalidSignature = errors.New("github webhook: invalid signature")

// ErrMissingHeader is returned when a required webhook header is absent.
var ErrMissingHeader = errors.New("github webhook: missing header")

// ErrReplay is returned when the X-GitHub-Delivery ID has already been
// seen inside the replay window.
var ErrReplay = errors.New("github webhook: duplicate delivery")

// ReplayStore tracks delivery IDs for replay protection. Production
// implementations live alongside Minos's Postgres (shared `cerberus`
// schema); tests use an in-memory map.
type ReplayStore interface {
	// Seen records delivery within the configured window and returns true
	// if this delivery had been observed before.
	Seen(ctx context.Context, delivery string, at time.Time) (bool, error)
}

// Verifier authenticates GitHub webhook requests.
type Verifier struct {
	secret []byte
	store  ReplayStore
	now    func() time.Time
}

// NewVerifier constructs a Verifier. secret is the App's webhook secret
// (not the installation token). store may be nil to disable replay
// protection for tests; production callers must pass a real ReplayStore.
func NewVerifier(secret []byte, store ReplayStore) *Verifier {
	return &Verifier{
		secret: secret,
		store:  store,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// WithClock overrides the Verifier's clock — for deterministic tests.
func (v *Verifier) WithClock(now func() time.Time) *Verifier {
	v.now = now
	return v
}

// Event is a parsed webhook event ready for handlers.
type Event struct {
	Type       string
	DeliveryID string
	Body       []byte
}

// Verify consumes the request, validates the signature and delivery ID,
// and returns a parsed Event. The request body is fully drained — callers
// MUST NOT read from r.Body afterwards.
func (v *Verifier) Verify(ctx context.Context, r *http.Request) (*Event, error) {
	if v == nil || len(v.secret) == 0 {
		return nil, errors.New("github webhook: verifier not configured")
	}

	sigHeader := r.Header.Get("X-Hub-Signature-256")
	if sigHeader == "" {
		return nil, fmt.Errorf("%w: X-Hub-Signature-256", ErrMissingHeader)
	}
	delivery := r.Header.Get("X-GitHub-Delivery")
	if delivery == "" {
		return nil, fmt.Errorf("%w: X-GitHub-Delivery", ErrMissingHeader)
	}
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "" {
		return nil, fmt.Errorf("%w: X-GitHub-Event", ErrMissingHeader)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("github webhook: read body: %w", err)
	}

	if err := verifyHMAC(v.secret, body, sigHeader); err != nil {
		return nil, err
	}

	if v.store != nil {
		seen, err := v.store.Seen(ctx, delivery, v.now())
		if err != nil {
			return nil, fmt.Errorf("github webhook: replay store: %w", err)
		}
		if seen {
			return nil, fmt.Errorf("%w: %s", ErrReplay, delivery)
		}
	}

	return &Event{
		Type:       eventType,
		DeliveryID: delivery,
		Body:       body,
	}, nil
}

// verifyHMAC is separated so unit tests can exercise edge cases without
// building an http.Request every time.
func verifyHMAC(secret, body []byte, header string) error {
	prefix := "sha256="
	if !strings.HasPrefix(header, prefix) {
		return fmt.Errorf("%w: header missing sha256= prefix", ErrInvalidSignature)
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return fmt.Errorf("%w: header not hex", ErrInvalidSignature)
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	want := mac.Sum(nil)
	if !hmac.Equal(got, want) {
		return ErrInvalidSignature
	}
	return nil
}
