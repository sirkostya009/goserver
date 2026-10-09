package domain

import "context"

// Credential is how a request proved who it is.
type Credential uint8

const (
	CredentialSession Credential = iota + 1
	CredentialAPIKey
	// CredentialAdmin is the operator's token. It acts as nobody, in whichever
	// tenant the request names.
	CredentialAdmin
)

// Principal is who a request acts as. The http layer puts it in the request's
// context once, the tenant it belongs to next to it.
type Principal struct {
	Via Credential
	// UserID is the user a session or an api key acts as. Zero for the admin.
	UserID ID
	// CredentialID is the session's or the key's own id.
	CredentialID ID
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, &p)
}

func PrincipalFrom(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(*Principal)
	return p, ok
}
