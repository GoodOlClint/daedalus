package jwt

import (
	"errors"
	"fmt"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

// ErrInvalidBearer is returned by VerifyBearer when the token fails any
// signature, expiry, or claim-shape check.
var ErrInvalidBearer = errors.New("invalid bearer token")

// SignBearer issues a Phase 1 bearer token: a JWT with HS256 signature over
// the Claims. Phase 2 swaps the algorithm for Ed25519 (EdDSA) without
// changing the claim shape — signature swap only.
func SignBearer(secret []byte, c Claims) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("%w: empty signing secret", ErrInvalidBearer)
	}
	mc := gojwt.MapClaims{
		"sub":        c.Subject,
		"iss":        c.Issuer,
		"aud":        c.Audience,
		"iat":        c.IssuedAt.Unix(),
		"exp":        c.Expires.Unix(),
		"jti":        c.JTI,
		"mcp_scopes": c.McpScopes,
	}
	tok := gojwt.NewWithClaims(gojwt.SigningMethodHS256, mc)
	return tok.SignedString(secret)
}

// VerifyBearer validates a token produced by SignBearer (or any compatible
// issuer using the same secret) and returns the extracted Claims. Expiry
// and signature are both checked; missing or malformed claim fields yield
// ErrInvalidBearer.
func VerifyBearer(secret []byte, token string) (*Claims, error) {
	if len(secret) == 0 {
		return nil, fmt.Errorf("%w: empty verification secret", ErrInvalidBearer)
	}
	parsed, err := gojwt.Parse(token, func(t *gojwt.Token) (any, error) {
		if _, ok := t.Method.(*gojwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	}, gojwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBearer, err)
	}
	mc, ok := parsed.Claims.(gojwt.MapClaims)
	if !ok || !parsed.Valid {
		return nil, fmt.Errorf("%w: claim type or validity", ErrInvalidBearer)
	}
	return mapToClaims(mc)
}

func mapToClaims(mc gojwt.MapClaims) (*Claims, error) {
	c := &Claims{}
	if v, ok := mc["sub"].(string); ok {
		c.Subject = v
	}
	if v, ok := mc["iss"].(string); ok {
		c.Issuer = v
	}
	if v, ok := mc["aud"].([]any); ok {
		for _, a := range v {
			if s, ok := a.(string); ok {
				c.Audience = append(c.Audience, s)
			}
		}
	}
	if v, ok := mc["iat"].(float64); ok {
		c.IssuedAt = time.Unix(int64(v), 0).UTC()
	}
	if v, ok := mc["exp"].(float64); ok {
		c.Expires = time.Unix(int64(v), 0).UTC()
	}
	if v, ok := mc["jti"].(string); ok {
		c.JTI = v
	}
	if v, ok := mc["mcp_scopes"].(map[string]any); ok {
		c.McpScopes = make(map[string][]string, len(v))
		for broker, scopes := range v {
			ss, ok := scopes.([]any)
			if !ok {
				continue
			}
			list := make([]string, 0, len(ss))
			for _, s := range ss {
				if str, ok := s.(string); ok {
					list = append(list, str)
				}
			}
			c.McpScopes[broker] = list
		}
	}
	return c, nil
}
