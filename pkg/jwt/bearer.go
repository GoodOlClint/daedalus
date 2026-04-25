package jwt

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

// ErrInvalidBearer is returned by Verify when the token fails any
// signature, expiry, or claim-shape check.
var ErrInvalidBearer = errors.New("invalid bearer token")

// Sign issues a Minos-minted JWT with EdDSA (Ed25519) signature over
// the Claims. priv is Minos's signing private key (resolved from the
// secret provider at startup).
func Sign(priv ed25519.PrivateKey, c Claims) (string, error) {
	if len(priv) == 0 {
		return "", fmt.Errorf("%w: empty signing key", ErrInvalidBearer)
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
	tok := gojwt.NewWithClaims(gojwt.SigningMethodEdDSA, mc)
	return tok.SignedString(priv)
}

// Verify validates a JWT against pub (Minos's signing public key,
// distributed to brokers via the secret provider). Expiry, signing
// algorithm, and claim shape are all checked.
func Verify(pub ed25519.PublicKey, token string) (*Claims, error) {
	if len(pub) == 0 {
		return nil, fmt.Errorf("%w: empty verification key", ErrInvalidBearer)
	}
	parsed, err := gojwt.Parse(token, func(t *gojwt.Token) (any, error) {
		if _, ok := t.Method.(*gojwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return pub, nil
	}, gojwt.WithValidMethods([]string{"EdDSA"}))
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
