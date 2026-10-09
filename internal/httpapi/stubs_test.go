package httpapi

import (
	"bytes"
	"context"
	"iter"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"goserver/domain"
	"goserver/internal/auth"
	"goserver/internal/logger"
	"goserver/internal/service"
)

// request logs would bury test failures, so they go nowhere unless a test asks
// for them with captureLogs
var logs bytes.Buffer

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.DiscardHandler))
	os.Exit(m.Run())
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	logs.Reset()
	slog.SetDefault(slog.New(logger.NewDev(&logs, slog.LevelInfo)))
	t.Cleanup(func() { slog.SetDefault(slog.New(slog.DiscardHandler)) })
	return &logs
}

// The stubs answer with what a test put in them and remember what the handler
// passed down, so a test sees both directions of the wire: how a request was
// bound, and how a result was written.

type usersStub struct {
	users []domain.User
	user  domain.User
	err   error

	tenant domain.TenantID
	id     domain.ID
	in     domain.CreateUser
	filter domain.UserFilter
}

func (s *usersStub) List(ctx context.Context) ([]domain.User, error) {
	s.tenant = domain.MustTenant(ctx).ID
	return s.users, s.err
}

func (s *usersStub) Query(ctx context.Context, f domain.UserFilter) ([]domain.User, error) {
	s.tenant, s.filter = domain.MustTenant(ctx).ID, f
	return s.users, s.err
}

func (s *usersStub) Get(ctx context.Context, id domain.ID) (domain.User, error) {
	s.tenant, s.id = domain.MustTenant(ctx).ID, id
	return s.user, s.err
}

func (s *usersStub) Create(ctx context.Context, in domain.CreateUser) (domain.User, error) {
	s.tenant, s.in = domain.MustTenant(ctx).ID, in
	return s.user, s.err
}

func (s *usersStub) Delete(ctx context.Context, id domain.ID) error {
	s.tenant, s.id = domain.MustTenant(ctx).ID, id
	return s.err
}

type postsStub struct {
	posts []domain.Post
	post  domain.Post
	err   error

	tenant   domain.TenantID
	id       domain.ID
	authorID domain.ID
	in       domain.CreatePost
	filter   domain.PostFilter

	imported  domain.PostImport
	unbatched bool
	read      []domain.CreatePost // what the import's body yielded, in order
	readErrs  []error
}

func (s *postsStub) List(ctx context.Context) ([]domain.Post, error) {
	s.tenant = domain.MustTenant(ctx).ID
	return s.posts, s.err
}

func (s *postsStub) Query(ctx context.Context, f domain.PostFilter) ([]domain.Post, error) {
	s.tenant, s.filter = domain.MustTenant(ctx).ID, f
	return s.posts, s.err
}

func (s *postsStub) ListByAuthor(ctx context.Context, authorID domain.ID) ([]domain.Post, error) {
	s.tenant, s.authorID = domain.MustTenant(ctx).ID, authorID
	return s.posts, s.err
}

func (s *postsStub) Get(ctx context.Context, id domain.ID) (domain.Post, error) {
	s.tenant, s.id = domain.MustTenant(ctx).ID, id
	return s.post, s.err
}

func (s *postsStub) Create(ctx context.Context, in domain.CreatePost) (domain.Post, error) {
	s.tenant, s.in = domain.MustTenant(ctx).ID, in
	return s.post, s.err
}

func (s *postsStub) Import(ctx context.Context, in iter.Seq2[domain.CreatePost, error]) (domain.PostImport, error) {
	s.tenant = domain.MustTenant(ctx).ID
	for p, err := range in {
		s.read, s.readErrs = append(s.read, p), append(s.readErrs, err)
	}
	return s.imported, s.err
}

func (s *postsStub) ImportUnbatched(ctx context.Context, in iter.Seq2[domain.CreatePost, error]) (domain.PostImport, error) {
	s.unbatched = true
	return s.Import(ctx, in)
}

func (s *postsStub) Delete(ctx context.Context, id domain.ID) error {
	s.tenant, s.id = domain.MustTenant(ctx).ID, id
	return s.err
}

type tenantsStub struct {
	tenants []domain.Tenant
	tenant  domain.Tenant
	err     error

	in     domain.CreateTenant
	filter domain.TenantFilter
}

func (s *tenantsStub) List(context.Context) ([]domain.Tenant, error) {
	return s.tenants, s.err
}

func (s *tenantsStub) Query(_ context.Context, f domain.TenantFilter) ([]domain.Tenant, error) {
	s.filter = f
	return s.tenants, s.err
}

func (s *tenantsStub) Create(_ context.Context, in domain.CreateTenant) (domain.Tenant, error) {
	s.in = in
	return s.tenant, s.err
}

// authStub accepts any token auth.ParseToken does, as guy, in the tenant the
// token names, unless reject says otherwise.
type authStub struct {
	reject  bool
	session service.Session
	user    domain.User
	err     error

	login     domain.Login
	loggedOut bool
}

func (s *authStub) Login(_ context.Context, in domain.Login) (service.Session, error) {
	s.login = in
	return s.session, s.err
}

func (s *authStub) Logout(ctx context.Context) error {
	if _, ok := domain.PrincipalFrom(ctx); !ok {
		return domain.ErrUnauthenticated
	}
	s.loggedOut = true
	return s.err
}

func (s *authStub) Me(ctx context.Context) (domain.User, error) {
	if _, ok := domain.PrincipalFrom(ctx); !ok {
		return domain.User{}, domain.ErrUnauthenticated
	}
	return s.user, s.err
}

func (s *authStub) Authenticate(ctx context.Context, token string) (context.Context, error) {
	kind, tenant, _, ok := auth.ParseToken(token)
	if !ok || s.reject {
		return ctx, domain.ErrUnauthenticated
	}
	via := domain.CredentialAPIKey
	if kind == auth.Session {
		via = domain.CredentialSession
	}
	ctx = domain.WithTenant(ctx, tenant)
	return domain.WithPrincipal(ctx, domain.Principal{Via: via, UserID: guyID, CredentialID: keyID}), nil
}

type keysStub struct {
	keys []domain.APIKey
	key  domain.NewAPIKey
	err  error

	principal domain.Principal
	tenant    domain.TenantID
	id        domain.ID
	in        domain.CreateAPIKey
}

func (s *keysStub) List(ctx context.Context) ([]domain.APIKey, error) {
	s.saw(ctx)
	return s.keys, s.err
}

func (s *keysStub) Create(ctx context.Context, in domain.CreateAPIKey) (domain.NewAPIKey, error) {
	s.saw(ctx)
	s.in = in
	return s.key, s.err
}

func (s *keysStub) Revoke(ctx context.Context, id domain.ID) error {
	s.saw(ctx)
	s.id = id
	return s.err
}

func (s *keysStub) saw(ctx context.Context) {
	s.principal, s.tenant = *must(domain.PrincipalFrom(ctx)), domain.MustTenant(ctx).ID
}

func must[T any](v T, ok bool) T {
	if !ok {
		panic("stub: context lacks what the guard promised")
	}
	return v
}

type pingStub struct{ err error }

func (p pingStub) Ping(context.Context) error { return p.err }

type harness struct {
	*Server

	users   *usersStub
	posts   *postsStub
	tenants *tenantsStub
	auth    *authStub
	keys    *keysStub
}

const adminToken = "admin-token-long-enough-to-pass-the-minimum"

func newHarness() harness {
	h := harness{users: &usersStub{}, posts: &postsStub{}, tenants: &tenantsStub{}, auth: &authStub{}, keys: &keysStub{}}
	services := service.Container{Tenants: h.tenants, Users: h.users, Posts: h.posts, Auth: h.auth, APIKeys: h.keys}
	h.Server = New(pingStub{}, services, adminToken)
	h.obs = newInstruments(tracenoop.NewTracerProvider(), metricnoop.NewMeterProvider(), propagation.TraceContext{})
	return h
}

// fixed values with their wire spelling written out, so the tests pin the
// encoding instead of re-deriving it
var (
	acme      = domain.TenantID(uuid.MustParse("01a0c081-9683-7dff-a267-f090f4ce1d17"))
	guyID     = domain.ID(uuid.MustParse("01a0c091-9024-7c9a-9641-9da47e750ce7"))
	postID    = domain.ID(uuid.MustParse("01a0c091-903e-7ed1-9f84-a3adfe207ba0"))
	keyID     = domain.ID(uuid.MustParse("01a0c091-9040-7caa-9641-9da47e750ceb"))
	createdAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
)

const (
	acmeText      = "AaDAgZaDff-iZ_CQ9M4dFw"
	guyText       = "AaDAkZAkfJqWQZ2kfnUM5w"
	postText      = "AaDAkZA-ftGfhKOt_iB7oA"
	keyText       = "AaDAkZBAfKqWQZ2kfnUM6w"
	createdAtText = "2026-01-02T03:04:05Z"
)

// request is who calls, besides what: tenant is a user of that tenant holding an
// api key, admin is the operator. Neither is anonymous.
type request struct {
	method, path string
	tenant       string
	admin        bool
	body         string
	header       map[string]string
}

func (h harness) do(t *testing.T, r request) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), r.method, r.path, strings.NewReader(r.body))
	if r.tenant != "" {
		id, err := domain.ParseTenantID(r.tenant)
		if err != nil {
			t.Fatalf("request for tenant %q: %v", r.tenant, err)
		}
		token, _ := auth.NewToken(auth.APIKey, id)
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if r.admin {
		req.Header.Set("Authorization", "Bearer "+adminToken)
	}
	for k, v := range r.header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func wantJSON(t *testing.T, rec *httptest.ResponseRecorder, body string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Errorf("status %d, want 200: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type %q", ct)
	}
	if got := rec.Body.String(); got != body {
		t.Errorf("body\n got %s\nwant %s", got, body)
	}
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("status %d, want %d: %s", rec.Code, status, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("errors are plaintext, got content type %q", ct)
	}
	if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != msg {
		t.Errorf("message %q, want %q", got, msg)
	}
}
