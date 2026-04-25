// Package brokerauth is the JWT verification middleware every Zakros
// broker layers in front of its MCP handlers per architecture.md §6
// MCP Broker Authentication. It is shared so the verification rules
// stay consistent across Minos's bundled handlers, the github broker
// (Slice F), Hecate (H1), Apollo (H2), and Phase 3 brokers.
//
// The middleware checks signature, audience, expiry, and scope on
// every call; denied calls return 403 with a structured error and an
// audit event. Scope matching uses Claims.HasScope.
//
// Replay protection (jti tracking) is delegated to a ReplayStore the
// caller supplies, so brokers can pick the storage that fits their
// scale: in-memory for single-broker deploys, shared-Postgres for
// the Phase 2 broker fleet.
package brokerauth

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zakros-hq/zakros/pkg/audit"
	"github.com/zakros-hq/zakros/pkg/jwt"
)

// ReplayStore tracks recently seen JTIs to refuse replays. Brokers
// may supply a no-op implementation if their scopes are idempotent
// and replay isn't a concern.
type ReplayStore interface {
	// Seen reports whether jti has already been recorded; on first
	// sighting it records the jti with the supplied expiry. Returns
	// (true, nil) on replay, (false, nil) on first use.
	Seen(ctx context.Context, jti string, expires time.Time) (bool, error)
}

// NopReplayStore is the explicit "do not track replays" choice. Lets
// brokers be unambiguous about opting out instead of silently passing
// nil.
type NopReplayStore struct{}

// Seen always returns (false, nil).
func (NopReplayStore) Seen(context.Context, string, time.Time) (bool, error) {
	return false, nil
}

// Verifier is the per-broker middleware factory. One Verifier per
// broker name (e.g., "github", "hecate") — the audience check is
// scoped to that name.
type Verifier struct {
	// Broker is the name brokers identify as in the JWT audience claim
	// and mcp_scopes map. Required.
	Broker string

	// PublicKey is Minos's Ed25519 verification key. Required.
	PublicKey ed25519.PublicKey

	// Replay tracks JTIs across calls. Required (use NopReplayStore to
	// opt out explicitly).
	Replay ReplayStore

	// Audit emits one event per allowed/denied call. Required.
	Audit audit.Emitter

	// Now is the clock used for replay-window math. Defaults to
	// time.Now (UTC) when nil — overridable for deterministic tests.
	Now func() time.Time
}

// claimsContextKey is the type used to stash verified claims on the
// request context so handlers can read them without re-verifying.
type claimsContextKey struct{}

// ClaimsFromContext returns the verified claims a handler is running
// under, or nil when no Verifier middleware is in front of it.
func ClaimsFromContext(ctx context.Context) *jwt.Claims {
	c, _ := ctx.Value(claimsContextKey{}).(*jwt.Claims)
	return c
}

// Require returns a middleware that verifies the request's bearer
// against v's broker. The required scope is checked per-route — pass
// the operation name a successful caller would be invoking.
func (v *Verifier) Require(scope string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := v.now()
		raw := bearerFromHeader(r.Header.Get("Authorization"))
		if raw == "" {
			v.deny(r, "missing or malformed bearer", scope)
			writeError(w, http.StatusUnauthorized, "missing or malformed bearer")
			return
		}
		claims, err := jwt.Verify(v.PublicKey, raw)
		if err != nil {
			v.deny(r, "invalid signature", scope)
			writeError(w, http.StatusUnauthorized, "invalid bearer")
			return
		}
		if !claims.HasAudience(v.Broker) {
			v.deny(r, "audience mismatch", scope)
			writeError(w, http.StatusForbidden, "audience mismatch")
			return
		}
		if claims.Expires.Before(now) {
			v.deny(r, "expired", scope)
			writeError(w, http.StatusUnauthorized, "token expired")
			return
		}
		if scope != "" && !claims.HasScope(v.Broker, scope) {
			v.deny(r, "scope denied", scope)
			writeError(w, http.StatusForbidden, "scope denied")
			return
		}
		if claims.JTI != "" && v.Replay != nil {
			seen, err := v.Replay.Seen(r.Context(), claims.JTI, claims.Expires)
			if err != nil {
				v.deny(r, "replay-store error", scope)
				writeError(w, http.StatusInternalServerError, "replay store")
				return
			}
			if seen {
				v.deny(r, "replay", scope)
				writeError(w, http.StatusUnauthorized, "replay")
				return
			}
		}
		v.audit(r, "allowed", scope, claims)
		ctx := context.WithValue(r.Context(), claimsContextKey{}, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (v *Verifier) now() time.Time {
	if v.Now != nil {
		return v.Now().UTC()
	}
	return time.Now().UTC()
}

func (v *Verifier) deny(r *http.Request, reason, scope string) {
	v.audit(r, "denied:"+reason, scope, nil)
}

func (v *Verifier) audit(r *http.Request, outcome, scope string, claims *jwt.Claims) {
	if v.Audit == nil {
		return
	}
	fields := map[string]string{
		"broker": v.Broker,
		"path":   r.URL.Path,
		"scope":  scope,
	}
	if claims != nil {
		fields["sub"] = claims.Subject
		fields["jti"] = claims.JTI
	}
	v.Audit.Emit(audit.Event{
		Category: "broker-auth",
		Outcome:  outcome,
		Fields:   fields,
	})
}

func bearerFromHeader(h string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + escapeJSON(msg) + `"}`))
}

// escapeJSON is a tiny helper so the structured error body doesn't
// need a full encoding/json round-trip for short literal strings.
func escapeJSON(s string) string {
	if !strings.ContainsAny(s, `"\`) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Sentinel for callers that want to detect the "no audience" case
// programmatically — kept exported so brokers can match it via
// errors.Is when they implement custom denial paths.
var ErrAudienceMismatch = errors.New("brokerauth: audience mismatch")
