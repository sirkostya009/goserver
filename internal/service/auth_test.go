package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"goserver/domain"
	"goserver/internal/auth"
	"goserver/internal/service"
)

// login opens a session for acme's user with this address and returns its
// token and the context a request presenting it would carry.
func (f fixture) login(email string) (string, context.Context) {
	f.t.Helper()
	sess, err := f.Services.Auth.Login(f.t.Context(), domain.Login{Tenant: "acme", Email: email, Password: "hunter22"})
	if err != nil {
		f.t.Fatal(err)
	}
	return sess.Token, f.authenticate(sess.Token)
}

func (f fixture) authenticate(token string) context.Context {
	f.t.Helper()
	ctx, err := f.Services.Auth.Authenticate(f.t.Context(), token)
	if err != nil {
		f.t.Fatalf("authenticate %s: %v", token, err)
	}
	return ctx
}

func TestAuth_Login_OpensSession(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme := f.tenant("acme")
	guy := f.user(acme, "Guy", "guy@acme.io")

	sess, err := f.Services.Auth.Login(t.Context(), domain.Login{Tenant: "acme", Email: "guy@acme.io", Password: "hunter22"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sess.Token, string(auth.Session)) || sess.User != guy {
		t.Errorf("session %+v", sess)
	}
	if left := time.Until(sess.ExpiresAt); left < service.SessionTTL-time.Minute || left > service.SessionTTL {
		t.Errorf("expires in %s", left)
	}

	ctx := f.authenticate(sess.Token)
	p, ok := domain.PrincipalFrom(ctx)
	if !ok || p.Via != domain.CredentialSession || p.UserID != guy.ID {
		t.Errorf("principal %+v", p)
	}
	if domain.MustTenant(ctx).ID != domain.MustTenant(acme).ID {
		t.Error("the session's tenant is not the one logged into")
	}
	if me, err := f.Services.Auth.Me(ctx); err != nil || me != guy {
		t.Errorf("me: %+v %v", me, err)
	}
}

// what failed is nobody's business: every failure reads the same
func TestAuth_Login_Failures_AllAlike(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme := f.tenant("acme")
	f.tenant("globex")
	f.user(acme, "Guy", "guy@acme.io")
	nopw := f.user(acme, "Hank", "hank@acme.io")
	if _, err := f.DB.NewRaw("UPDATE "+domain.MustTenant(acme).Schema+".users SET password_hash = NULL WHERE id = ?", nopw.ID).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}

	var first error
	for name, in := range map[string]domain.Login{
		"wrong password":    {Tenant: "acme", Email: "guy@acme.io", Password: "hunter23"},
		"unknown email":     {Tenant: "acme", Email: "nobody@acme.io", Password: "hunter22"},
		"unknown tenant":    {Tenant: "initech", Email: "guy@acme.io", Password: "hunter22"},
		"another tenant":    {Tenant: "globex", Email: "guy@acme.io", Password: "hunter22"},
		"no password set":   {Tenant: "acme", Email: "hank@acme.io", Password: ""},
		"password prefixed": {Tenant: "acme", Email: "guy@acme.io", Password: "hunter2"},
	} {
		_, err := f.Services.Auth.Login(t.Context(), in)
		if !errors.Is(err, domain.ErrUnauthenticated) {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if first == nil {
			first = err
		} else if err.Error() != first.Error() {
			t.Errorf("%s: %q, others say %q", name, err, first)
		}
	}
}

// registered, not migrated: its schema is missing, which postgres rejects, so
// the test ends here
func TestAuth_Login_UnmigratedTenant(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if _, err := f.Services.Tenants.Create(t.Context(), domain.CreateTenant{Slug: "fresh", Name: "Fresh"}); err != nil {
		t.Fatal(err)
	}
	_, err := f.Services.Auth.Login(t.Context(), domain.Login{Tenant: "fresh", Email: "guy@fresh.io", Password: "hunter22"})
	if !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("login: %v", err)
	}
}

func TestAuth_Logout_EndsSession(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme := f.tenant("acme")
	f.user(acme, "Guy", "guy@acme.io")
	token, ctx := f.login("guy@acme.io")
	other, _ := f.login("guy@acme.io")

	if err := f.Services.Auth.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Services.Auth.Authenticate(t.Context(), token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("a logged out session still works: %v", err)
	}
	// a logout ends that session, not the user's others
	f.authenticate(other)
}

func TestAuth_Session_Expired(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme := f.tenant("acme")
	guy := f.user(acme, "Guy", "guy@acme.io")

	token, hash := auth.NewToken(auth.Session, domain.MustTenant(acme).ID)
	if _, err := f.Repos.Sessions.Insert(acme, guy.ID, hash, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Services.Auth.Authenticate(t.Context(), token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("an expired session works: %v", err)
	}

	// the next login sweeps it away
	f.login("guy@acme.io")
	n, err := f.DB.NewRaw("SELECT 1 FROM " + domain.MustTenant(acme).Schema + ".sessions").Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if rows, _ := n.RowsAffected(); rows != 1 {
		t.Errorf("%d sessions, want the new one alone", rows)
	}
}

func TestAuth_Authenticate_Garbage(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme := f.tenant("acme")
	f.user(acme, "Guy", "guy@acme.io")
	f.login("guy@acme.io")

	unissued, _ := auth.NewToken(auth.Session, domain.MustTenant(acme).ID)
	unissuedKey, _ := auth.NewToken(auth.APIKey, domain.MustTenant(acme).ID)
	for _, token := range []string{"", "nope", "gss_", unissued, unissuedKey} {
		if _, err := f.Services.Auth.Authenticate(t.Context(), token); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Errorf("%q: %v", token, err)
		}
	}
}

// a well-formed token naming a tenant that does not exist looks in a schema
// that does not either, which postgres rejects, so the test ends here
func TestAuth_Authenticate_UnknownTenant(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	token, _ := auth.NewToken(auth.Session, domain.TenantID(uuid.NewV7()))
	if _, err := f.Services.Auth.Authenticate(t.Context(), token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("unknown tenant: %v", err)
	}
}

func TestAPIKeys_Lifecycle(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme := f.tenant("acme")
	guy := f.user(acme, "Guy", "guy@acme.io")
	_, session := f.login("guy@acme.io")

	made, err := f.Services.APIKeys.Create(session, domain.CreateAPIKey{Name: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(made.Token, string(auth.APIKey)) || made.Key.UserID != guy.ID || made.Key.Name != "ci" || made.Key.ExpiresAt != nil {
		t.Errorf("key %+v", made)
	}

	keyCtx := f.authenticate(made.Token)
	if p, _ := domain.PrincipalFrom(keyCtx); p.Via != domain.CredentialAPIKey || p.UserID != guy.ID || p.CredentialID != made.Key.ID {
		t.Errorf("principal %+v", p)
	}
	if me, err := f.Services.Auth.Me(keyCtx); err != nil || me != guy {
		t.Errorf("me through the key: %+v %v", me, err)
	}
	if keys, err := f.Services.APIKeys.List(keyCtx); err != nil || len(keys) != 1 || keys[0] != made.Key {
		t.Errorf("list: %v %v", keys, err)
	}

	// a key can neither make keys nor unmake them, nor log out
	if _, err := f.Services.APIKeys.Create(keyCtx, domain.CreateAPIKey{Name: "more"}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a key made a key: %v", err)
	}
	if err := f.Services.APIKeys.Revoke(keyCtx, made.Key.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a key revoked a key: %v", err)
	}
	if err := f.Services.Auth.Logout(keyCtx); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a key logged out: %v", err)
	}

	if err := f.Services.APIKeys.Revoke(session, made.Key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Services.Auth.Authenticate(t.Context(), made.Token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("a revoked key still works: %v", err)
	}
}

func TestAPIKeys_Expiry(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme := f.tenant("acme")
	guy := f.user(acme, "Guy", "guy@acme.io")
	_, session := f.login("guy@acme.io")

	past := time.Now().Add(-time.Minute)
	if _, err := f.Services.APIKeys.Create(session, domain.CreateAPIKey{Name: "old", ExpiresAt: &past}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("made an expired key: %v", err)
	}

	future := time.Now().Add(time.Hour)
	live, err := f.Services.APIKeys.Create(session, domain.CreateAPIKey{Name: "live", ExpiresAt: &future})
	if err != nil {
		t.Fatal(err)
	}
	f.authenticate(live.Token)

	// one that expired since it was made, stored the way the service would have
	token, hash := auth.NewToken(auth.APIKey, domain.MustTenant(acme).ID)
	if _, err := f.Repos.APIKeys.Insert(acme, guy.ID, domain.CreateAPIKey{Name: "lapsed", ExpiresAt: &past}, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Services.Auth.Authenticate(t.Context(), token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("an expired key works: %v", err)
	}
}

// the lookup is by hash and expiry together: an unknown token must not find a
// live key that never expires
func TestAPIKeys_UnknownToken_BesideLiveKey(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme := f.tenant("acme")
	f.user(acme, "Guy", "guy@acme.io")
	_, session := f.login("guy@acme.io")
	if _, err := f.Services.APIKeys.Create(session, domain.CreateAPIKey{Name: "forever"}); err != nil {
		t.Fatal(err)
	}
	unissued, _ := auth.NewToken(auth.APIKey, domain.MustTenant(acme).ID)
	if _, err := f.Services.Auth.Authenticate(t.Context(), unissued); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("unissued token: %v", err)
	}
}

func TestAPIKeys_OnlyTheOwners(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme := f.tenant("acme")
	f.user(acme, "Guy", "guy@acme.io")
	f.user(acme, "Hank", "hank@acme.io")
	_, guys := f.login("guy@acme.io")
	_, hanks := f.login("hank@acme.io")

	key, err := f.Services.APIKeys.Create(guys, domain.CreateAPIKey{Name: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	if keys, err := f.Services.APIKeys.List(hanks); err != nil || len(keys) != 0 {
		t.Errorf("hank sees %v %v", keys, err)
	}
	if err := f.Services.APIKeys.Revoke(hanks, key.Key.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("hank revoked guy's key: %v", err)
	}
	f.authenticate(key.Token)
}

func TestAuth_Admin_IsNobody(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme := f.tenant("acme")
	admin := domain.WithPrincipal(acme, domain.Principal{Via: domain.CredentialAdmin})

	if _, err := f.Services.Auth.Me(admin); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("me: %v", err)
	}
	if _, err := f.Services.APIKeys.List(admin); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("list keys: %v", err)
	}
	if _, err := f.Services.APIKeys.Create(admin, domain.CreateAPIKey{Name: "x"}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("create key: %v", err)
	}
	if err := f.Services.Auth.Logout(admin); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("logout: %v", err)
	}
	if _, err := f.Services.Auth.Me(acme); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("me with nobody: %v", err)
	}
}

// credentials go with their user
func TestAuth_UserDeleted_CredentialsGone(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme := f.tenant("acme")
	guy := f.user(acme, "Guy", "guy@acme.io")
	session, ctx := f.login("guy@acme.io")
	key, err := f.Services.APIKeys.Create(ctx, domain.CreateAPIKey{Name: "ci"})
	if err != nil {
		t.Fatal(err)
	}

	if err := f.Services.Users.Delete(acme, guy.ID); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{session, key.Token} {
		if _, err := f.Services.Auth.Authenticate(t.Context(), token); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Errorf("%s outlived its user: %v", token[:4], err)
		}
	}
}

func TestUsers_Create_StoresHashNotPassword(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme := f.tenant("acme")
	f.user(acme, "Guy", "guy@acme.io")

	cred, err := f.Repos.Users.Credentials(acme, "guy@acme.io")
	if err != nil {
		t.Fatal(err)
	}
	// the hash's kind is auth's business, and the testhash build swaps it
	if cred.PasswordHash == nil || !strings.HasPrefix(*cred.PasswordHash, "$") || strings.Contains(*cred.PasswordHash, "hunter22") {
		t.Errorf("stored %v", cred.PasswordHash)
	}
	if _, err := f.Services.Users.Create(acme, domain.CreateUser{Name: "x", Email: "x@acme.io"}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a user without a password: %v", err)
	}
}
