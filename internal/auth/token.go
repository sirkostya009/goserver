package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/base64"
	"fmt"

	"goserver/domain"
)

// Kind is what a token opens, spelled as its prefix. A prefix also lets secret
// scanners recognise a leaked token.
type Kind string

const (
	Session Kind = "gss_"
	APIKey  Kind = "gsk_"
)

const (
	prefixLen = 4
	secretLen = 32
	rawLen    = len(domain.TenantID{}) + secretLen
	tokenLen  = prefixLen + rawLen*4/3 // base64 without padding: 48 bytes are 64 chars
)

// Hash is what the database keeps of a token: enough to find it, useless to
// present.
type Hash [sha256.Size]byte

// Scan reads a hash back from its bytea column, refusing one of any other length.
func (h *Hash) Scan(src any) error {
	b, ok := src.([]byte)
	if !ok || len(b) != len(h) {
		return fmt.Errorf("auth: cannot scan %T of %d bytes into a %d-byte hash", src, len(b), len(h))
	}
	copy(h[:], b)
	return nil
}

func (h Hash) Value() (driver.Value, error) { return h[:], nil }

// NewToken makes a token for tenant: its kind, then the tenant id in the clear
// and a random secret. Carrying the tenant keeps it a pure function of the
// token, so finding the schema the token lives in costs no lookup.
func NewToken(kind Kind, tenant domain.TenantID) (string, Hash) {
	var raw [rawLen]byte
	copy(raw[:], tenant[:])
	_, _ = rand.Read(raw[len(tenant):]) // never fails, see crypto/rand

	b := make([]byte, tokenLen)
	copy(b, kind)
	base64.RawURLEncoding.Encode(b[prefixLen:], raw[:])
	return string(b), sha256.Sum256(raw[:])
}

// ParseToken splits a token into what NewToken put in it. ok is false for
// anything NewToken could not have made.
func ParseToken(token string) (kind Kind, tenant domain.TenantID, hash Hash, ok bool) {
	if len(token) != tokenLen {
		return "", tenant, hash, false
	}
	kind = Kind(token[:prefixLen])
	if kind != Session && kind != APIKey {
		return "", tenant, hash, false
	}
	var raw [rawLen]byte
	if n, err := base64.RawURLEncoding.Strict().Decode(raw[:], []byte(token[prefixLen:])); err != nil || n != rawLen {
		return "", tenant, hash, false
	}
	copy(tenant[:], raw[:])
	return kind, tenant, sha256.Sum256(raw[:]), true
}
