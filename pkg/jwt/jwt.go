// Package jwt handles pod-to-broker authentication via Minos-signed
// Ed25519 JWTs per architecture.md §6 MCP Broker Authentication.
//
// Minos holds the Ed25519 private key and signs one JWT per pod at
// spawn. Brokers hold the matching public key (distributed via the
// configured secret provider at broker startup) and verify on every
// request. Scope enforcement is per-call: the broker compares the
// requested operation against the JWT's `mcp_scopes[<broker>]` list.
//
// Key rotation is the emergency-revocation lever: rotate Minos's
// signing key and every outstanding JWT becomes unverifiable
// simultaneously. The previous private key may stay loaded as a
// verification-only fallback during a brief grace window if needed,
// though Phase 2 ships a hard-cutover rotation primitive.
package jwt

import "time"

// Claims is the JWT body shape brokers verify. Subject, Issuer,
// Audience, IssuedAt, Expires, JTI, and McpScopes match the design
// in architecture.md §6 MCP Broker Authentication.
type Claims struct {
	Subject  string    `json:"sub"`
	Issuer   string    `json:"iss"`
	Audience []string  `json:"aud"`
	IssuedAt time.Time `json:"iat"`
	Expires  time.Time `json:"exp"`
	JTI      string    `json:"jti"`
	// McpScopes maps broker name to the allowed operation strings for this
	// pod on that broker. Keys match audience entries.
	McpScopes map[string][]string `json:"mcp_scopes"`
}

// HasScope reports whether the caller is permitted to invoke op on broker.
// Used by broker-side middleware before dispatching to the handler.
func (c *Claims) HasScope(broker, op string) bool {
	if c == nil {
		return false
	}
	for _, s := range c.McpScopes[broker] {
		if s == op {
			return true
		}
	}
	return false
}

// HasAudience reports whether broker is in the JWT's audience claim.
// Brokers refuse tokens not addressed to them even if scopes happen to
// be set — a stricter check that closes a confused-deputy gap.
func (c *Claims) HasAudience(broker string) bool {
	if c == nil {
		return false
	}
	for _, a := range c.Audience {
		if a == broker {
			return true
		}
	}
	return false
}
