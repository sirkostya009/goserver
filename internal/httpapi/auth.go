package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"goserver/domain"
	"goserver/internal/auth"
	"goserver/internal/service"
)

const (
	// __Host- makes the browser refuse the cookie unless it is Secure, for the
	// whole site and for this host alone, so no sibling subdomain can plant one
	sessionCookie = "__Host-session"
	tenantHeader  = "X-Tenant-Id"
)

// AuthApi opens and closes sessions. It is not guarded: logging in is how a
// caller gets past the guards.
type AuthApi struct {
	Auth service.AuthService
}

// Login opens a session and sets it as the cookie the browser then sends by
// itself.
//
//rr:route POST /api/auth/login
func (a *AuthApi) Login(w http.ResponseWriter, ctx context.Context, body domain.Login) (domain.User, error) {
	sess, err := a.Auth.Login(ctx, body)
	if err != nil {
		return domain.User{}, err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sess.Token,
		Path:     "/",
		MaxAge:   int(time.Until(sess.ExpiresAt).Seconds()),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Cache-Control", "no-store")
	return sess.User, nil
}

// Logout ends the session and clears its cookie.
//
//rr:route POST /api/auth/logout
func (a *AuthApi) Logout(w http.ResponseWriter, ctx context.Context) error {
	// a session deleted meanwhile is as logged out as it gets
	if err := a.Auth.Logout(ctx); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	return nil
}

//rr:route GET /api/auth/me
func (a *AuthApi) Me(ctx context.Context) (domain.User, error) {
	return a.Auth.Me(ctx)
}

// authenticate resolves the request's credential into the tenant and principal
// ctx carries. A credential that opens nothing leaves the request anonymous
// rather than failing it, so a stale cookie never stands between a user and
// logging in again: the guards turn the anonymous away where it matters.
func (s *Server) authenticate(ctx context.Context, r *http.Request) (context.Context, error) {
	token, bearer := credential(r)
	switch {
	case token == "":
		return ctx, nil
	case bearer && s.isAdmin(token):
		return withTenant(domain.WithPrincipal(ctx, domain.Principal{Via: domain.CredentialAdmin}), r), nil
	// sessions ride in the cookie, keys in the header, and never the other way
	// round: a key must not become ambient like a cookie, and a session must
	// not leave the browser's jar
	case bearer == strings.HasPrefix(token, string(auth.Session)):
		return ctx, nil
	}
	authed, err := s.Auth.Authenticate(ctx, token)
	if errors.Is(err, domain.ErrUnauthenticated) {
		return ctx, nil
	}
	return authed, err
}

// credential is the token the request presents, and whether it came as a
// bearer token rather than the session cookie. The header wins if both came.
func credential(r *http.Request) (string, bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		const scheme = "Bearer "
		if len(h) > len(scheme) && strings.EqualFold(h[:len(scheme)], scheme) {
			return h[len(scheme):], true
		}
		return "", true
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value, false
	}
	return "", false
}

func (s *Server) isAdmin(token string) bool {
	if s.adminHash == nil {
		return false
	}
	h := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(h[:], s.adminHash) == 1
}

// withTenant is how the admin names the tenant it acts in, which a user's token
// names for them. A missing or malformed header is left for requireTenant.
func withTenant(ctx context.Context, r *http.Request) context.Context {
	id, err := domain.ParseTenantID(r.Header.Get(tenantHeader))
	if err != nil {
		return ctx
	}
	return domain.WithTenant(ctx, id)
}

// requireTenant guards tenant-scoped controllers: past it, the context carries
// a principal and the tenant it acts in.
func requireTenant(w http.ResponseWriter, r *http.Request) bool {
	ctx := r.Context()
	if _, ok := domain.PrincipalFrom(ctx); !ok {
		unauthenticated(w, domain.ErrUnauthenticated)
		return false
	}
	if _, ok := domain.TenantFrom(ctx); ok {
		return true
	}
	// only the admin gets here without one: it names its tenant per request
	if r.Header.Get(tenantHeader) == "" {
		http.Error(w, "missing X-Tenant-ID header", http.StatusBadRequest)
	} else {
		http.Error(w, domain.ErrBadID.Error(), http.StatusBadRequest)
	}
	return false
}

// requireAdmin guards the operator's controllers.
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	p, ok := domain.PrincipalFrom(r.Context())
	switch {
	case !ok:
		unauthenticated(w, domain.ErrUnauthenticated)
		return false
	case p.Via != domain.CredentialAdmin:
		http.Error(w, domain.ErrForbidden.Error(), http.StatusForbidden)
		return false
	}
	return true
}

func unauthenticated(w http.ResponseWriter, err error) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, err.Error(), http.StatusUnauthorized)
}
