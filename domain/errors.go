package domain

import "errors"

// Sentinels the http layer maps to status codes. Wrap them with %w to add detail.
var (
	ErrNotFound      = errors.New("not found")
	ErrConflict      = errors.New("already exists")
	ErrInvalid       = errors.New("invalid input")
	ErrUnknownTenant = errors.New("tenant not found")
	// ErrMalformed is a body that could not be read at all, as rr answers one
	// it decodes itself. A handler that reads its own body returns it.
	ErrMalformed = errors.New("malformed body")
	// ErrUnauthenticated is a request that proved nobody: no credential, or one
	// that is wrong, expired or revoked.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrForbidden is a request that proved who it is, and that is not allowed.
	ErrForbidden = errors.New("forbidden")
)
