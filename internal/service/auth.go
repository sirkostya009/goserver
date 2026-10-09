package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"goserver/domain"
	"goserver/internal/auth"
	"goserver/internal/repository"
)

// SessionTTL is how long a login lasts. It does not slide: a session ends this
// long after it began, however busy it was.
const SessionTTL = 30 * 24 * time.Hour

type AuthService interface {
	// Login checks a password and opens a session. A wrong tenant, address or
	// password all fail alike, and take as long.
	Login(ctx context.Context, in domain.Login) (Session, error)
	// Logout ends the session the caller presented.
	Logout(ctx context.Context) error
	// Me is the user the caller acts as.
	Me(ctx context.Context) (domain.User, error)
	// Authenticate resolves a token to who presents it. The context it returns
	// carries the token's tenant and principal. A token that opens nothing is
	// ErrUnauthenticated.
	Authenticate(ctx context.Context, token string) (context.Context, error)
}

// Session is a login just made: the token to hand the client, once.
type Session struct {
	Token     string
	ExpiresAt time.Time
	User      domain.User
}

var errBadLogin = fmt.Errorf("%w: wrong tenant, email or password", domain.ErrUnauthenticated)

type authService struct {
	tenants  repository.TenantRepository
	users    repository.UserRepository
	sessions repository.SessionRepository
	keys     repository.APIKeyRepository
}

func NewAuthService(tenants repository.TenantRepository, users repository.UserRepository, sessions repository.SessionRepository, keys repository.APIKeyRepository) AuthService {
	return &authService{tenants: tenants, users: users, sessions: sessions, keys: keys}
}

func (s *authService) Login(ctx context.Context, in domain.Login) (Session, error) {
	tenant, err := s.tenants.BySlug(ctx, in.Tenant)
	if errors.Is(err, domain.ErrNotFound) {
		return Session{}, s.fail(ctx, in.Password)
	}
	if err != nil {
		return Session{}, err
	}
	ctx = domain.WithTenant(ctx, tenant.ID)

	cred, err := s.users.Credentials(ctx, in.Email)
	switch {
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrUnknownTenant):
		return Session{}, s.fail(ctx, in.Password)
	case err != nil:
		return Session{}, err
	case cred.PasswordHash == nil:
		return Session{}, s.fail(ctx, in.Password)
	}
	ok, err := auth.VerifyPassword(ctx, *cred.PasswordHash, in.Password)
	if err != nil {
		return Session{}, err
	}
	if !ok {
		return Session{}, errBadLogin
	}

	// each login sweeps the user's dead sessions, so they cannot pile up
	if err := s.sessions.DeleteExpired(ctx, cred.ID); err != nil {
		return Session{}, err
	}
	token, hash := auth.NewToken(auth.Session, tenant.ID)
	sess, err := s.sessions.Insert(ctx, cred.ID, hash, time.Now().Add(SessionTTL))
	if err != nil {
		return Session{}, err
	}
	return Session{Token: token, ExpiresAt: sess.ExpiresAt, User: cred.User}, nil
}

func (s *authService) Logout(ctx context.Context) error {
	p, err := caller(ctx)
	if err != nil {
		return err
	}
	if p.Via != domain.CredentialSession {
		return fmt.Errorf("%w: only a session logs out, revoke an api key instead", domain.ErrForbidden)
	}
	return s.sessions.Delete(ctx, p.CredentialID)
}

func (s *authService) Me(ctx context.Context) (domain.User, error) {
	p, err := caller(ctx)
	if err != nil {
		return domain.User{}, err
	}
	return s.users.Get(ctx, p.UserID)
}

func (s *authService) Authenticate(ctx context.Context, token string) (context.Context, error) {
	kind, tenant, hash, ok := auth.ParseToken(token)
	if !ok {
		return ctx, domain.ErrUnauthenticated
	}
	ctx = domain.WithTenant(ctx, tenant)

	var p domain.Principal
	var err error
	switch kind {
	case auth.Session:
		var sess repository.Session
		sess, err = s.sessions.ByHash(ctx, hash)
		p = domain.Principal{Via: domain.CredentialSession, UserID: sess.UserID, CredentialID: sess.ID}
	case auth.APIKey:
		var key domain.APIKey
		key, err = s.keys.ByHash(ctx, hash)
		p = domain.Principal{Via: domain.CredentialAPIKey, UserID: key.UserID, CredentialID: key.ID}
	}
	// the tenant is whatever the token claims, so one that is gone or never was
	// is just a token that opens nothing
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrUnknownTenant) {
		return ctx, domain.ErrUnauthenticated
	}
	if err != nil {
		return ctx, err
	}
	return domain.WithPrincipal(ctx, p), nil
}

// fail burns the time a password check takes, then fails the login.
func (s *authService) fail(ctx context.Context, password string) error {
	if _, err := auth.VerifyPassword(ctx, "", password); err != nil {
		return err
	}
	return errBadLogin
}

// caller is the user the request acts as. The admin is nobody, so it is
// forbidden what only a user can do.
func caller(ctx context.Context) (*domain.Principal, error) {
	p, ok := domain.PrincipalFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthenticated
	}
	if p.Via == domain.CredentialAdmin {
		return nil, fmt.Errorf("%w: the admin is not a user", domain.ErrForbidden)
	}
	return p, nil
}
