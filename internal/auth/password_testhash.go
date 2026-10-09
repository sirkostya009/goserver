//go:build testhash

// Package auth holds what credentials are made of: password hashes, and the
// opaque tokens that sessions and api keys hand out.
//
// This is the testhash build: an unsalted sha-256 in place of argon2id, so
// tests that make users do not pay a memory-hard hash for each. Nothing built
// with it may hold a real password.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strings"
)

const testhashPrefix = "$testhash$"

func HashPassword(_ context.Context, password string) (string, error) {
	sum := sha256.Sum256([]byte(password))
	return testhashPrefix + base64.RawStdEncoding.EncodeToString(sum[:]), nil
}

func VerifyPassword(ctx context.Context, encoded, password string) (bool, error) {
	if !strings.HasPrefix(encoded, testhashPrefix) {
		return false, nil
	}
	want, _ := HashPassword(ctx, password)
	return subtle.ConstantTimeCompare([]byte(encoded), []byte(want)) == 1, nil
}
