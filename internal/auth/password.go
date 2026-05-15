// Package auth contains password hashing, session creation, and middleware
// helpers used by the server.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. Tuned for the home-server CPU: ~50–80 ms per hash on a
// modest x86 box, fast enough for interactive login, slow enough to make a
// dictionary attack expensive. Match HCT's parameters byte-for-byte.
const (
	argonMemory  = 64 * 1024 // KiB
	argonTime    = 2
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// HashPassword returns an encoded argon2id hash of the form:
//
//	$argon2id$v=19$m=65536,t=2,p=2$<salt-b64>$<key-b64>
func HashPassword(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("empty password")
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(plain), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	enc := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
	return enc, nil
}

// VerifyPassword returns nil on match. Constant-time compare.
func VerifyPassword(encoded, plain string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return errors.New("invalid hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return fmt.Errorf("parse version: %w", err)
	}
	if version != argon2.Version {
		return errors.New("unsupported argon2 version")
	}
	var mem uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return fmt.Errorf("parse params: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return fmt.Errorf("decode salt: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return fmt.Errorf("decode key: %w", err)
	}
	got := argon2.IDKey([]byte(plain), salt, t, mem, p, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return errors.New("password mismatch")
	}
	return nil
}
