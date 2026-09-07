package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

// NewSessionToken returns a 32-byte random token hex-encoded (64 chars).
// Used as the cookie value. Stored as plaintext in the DB because it is itself
// already a high-entropy random secret; an attacker with read access to the
// DB can already impersonate users anyway.
func NewSessionToken() (string, error) {
	return randomHex(32)
}

// NewServiceToken returns a fresh token plaintext and its sha256 hash.
// Only the hash is persisted; the plaintext is shown once to the human.
// Prefix matches the project: ft_st_ → "FT service token".
func NewServiceToken() (plaintext, hashHex string, err error) {
	return newPrefixedToken("ft_st_")
}

// NewReadOnlyToken returns a fresh ft_mcp_-prefixed token plaintext and its
// sha256 hash (SC-41). Mint it with scopes=["read"] via CreateServiceToken —
// the prefix is a grep-friendly label, not enforcement; the actual read-only
// guarantee comes from requireReadToken (server/middleware.go) checking the
// scope, plus /mcp never wiring in a mutating store call.
func NewReadOnlyToken() (plaintext, hashHex string, err error) {
	return newPrefixedToken("ft_mcp_")
}

func newPrefixedToken(prefix string) (plaintext, hashHex string, err error) {
	p, err := randomHex(32)
	if err != nil {
		return "", "", err
	}
	p = prefix + p // grep-friendly prefix
	sum := sha256.Sum256([]byte(p))
	return p, hex.EncodeToString(sum[:]), nil
}

// HashServiceToken returns the sha256 hex of an incoming token for DB lookup.
func HashServiceToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
