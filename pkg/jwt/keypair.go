package jwt

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

// ErrKeyMaterial is returned when key generation, parsing, or encoding
// fails — including malformed PEM, wrong key type, or short reads.
var ErrKeyMaterial = errors.New("invalid key material")

// GenerateKeypair returns a fresh Ed25519 keypair suitable for Minos's
// signing key. Source of entropy is crypto/rand.
func GenerateKeypair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: generate ed25519: %v", ErrKeyMaterial, err)
	}
	return pub, priv, nil
}

// MarshalPrivateKey encodes a private key as PEM (PKCS#8 inside).
// This is the form Minos persists to its secret provider.
func MarshalPrivateKey(priv ed25519.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal private: %v", ErrKeyMaterial, err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// MarshalPublicKey encodes a public key as PEM (SubjectPublicKeyInfo).
// Brokers consume this at startup via their secret provider.
func MarshalPublicKey(pub ed25519.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal public: %v", ErrKeyMaterial, err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

// ParsePrivateKey decodes a PEM-encoded Ed25519 private key. Returns
// ErrKeyMaterial for any decoding failure or wrong key type.
func ParsePrivateKey(data []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%w: no PEM block in private key data", ErrKeyMaterial)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parse pkcs8: %v", ErrKeyMaterial, err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%w: not an ed25519 private key", ErrKeyMaterial)
	}
	return priv, nil
}

// ParsePublicKey decodes a PEM-encoded Ed25519 public key.
func ParsePublicKey(data []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%w: no PEM block in public key data", ErrKeyMaterial)
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parse pkix: %v", ErrKeyMaterial, err)
	}
	pub, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%w: not an ed25519 public key", ErrKeyMaterial)
	}
	return pub, nil
}
