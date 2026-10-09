package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"uuid"

	"goserver/domain"
)

func TestHealth_OKAndFailing(t *testing.T) {
	t.Parallel()
	h := newHarness()
	wantJSON(t, h.do(t, request{method: "GET", path: "/api/health"}), `{"status":"ok"}`)

	h.DB = pingStub{err: errors.New("connection refused")}
	wantError(t, h.do(t, request{method: "GET", path: "/api/health"}), http.StatusInternalServerError, "internal error")
}

// health answers anybody, whatever credential they bring or do not
func TestHealth_IgnoresCredentials(t *testing.T) {
	t.Parallel()
	h := newHarness()
	for _, r := range []request{
		{},
		{tenant: acmeText},
		{admin: true},
		{header: map[string]string{"Authorization": "Bearer nope"}},
		{header: map[string]string{"Cookie": sessionCookie + "=nope"}},
	} {
		r.method, r.path = "GET", "/api/health"
		wantJSON(t, h.do(t, r), `{"status":"ok"}`)
	}
}

// the anonymous, and whoever presents a credential that opens nothing, are
// turned away before any service sees them
func TestTenantRoutes_NeedCredential(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/api/users", "/api/posts", "/api/users/" + guyText, "/api/posts/" + postText, "/api/keys"} {
		h := newHarness()
		h.auth.reject = true
		for _, header := range []map[string]string{
			nil,
			{"Authorization": "Bearer nope"},
			{"Authorization": "Basic Z3V5Omh1bnRlcjIy"},
			{"Authorization": "Bearer "},
			{"Cookie": sessionCookie + "=nope"},
			// a user's credential names its own tenant, the header is the admin's
			{"X-Tenant-Id": acmeText},
		} {
			rec := h.do(t, request{method: "GET", path: path, header: header})
			wantError(t, rec, http.StatusUnauthorized, "unauthenticated")
		}
		rec := h.do(t, request{method: "GET", path: path, tenant: acmeText})
		wantError(t, rec, http.StatusUnauthorized, "unauthenticated")
		if h.users.tenant != (domain.TenantID{}) || h.posts.tenant != (domain.TenantID{}) || h.keys.tenant != (domain.TenantID{}) {
			t.Errorf("%s: an unauthenticated request reached a service", path)
		}
	}
}

// the admin has no tenant of its own, so it names one per request
func TestAdmin_TenantHeader(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/api/users", "/api/posts", "/api/users/" + guyText, "/api/posts/" + postText} {
		h := newHarness()
		wantError(t, h.do(t, request{method: "GET", path: path, admin: true}), http.StatusBadRequest, "missing X-Tenant-ID header")

		for _, bad := range []string{"nope", uuid.UUID(acme).String(), acmeText + "A", acmeText[:21] + "=", acmeText[:21] + "x"} {
			rec := h.do(t, request{method: "GET", path: path, admin: true, header: map[string]string{"X-Tenant-Id": bad}})
			wantError(t, rec, http.StatusBadRequest, domain.ErrBadID.Error())
		}
		if h.users.tenant != (domain.TenantID{}) || h.posts.tenant != (domain.TenantID{}) {
			t.Errorf("%s: a rejected tenant reached a service", path)
		}

		h.do(t, request{method: "GET", path: path, admin: true, header: map[string]string{"X-Tenant-Id": acmeText}})
		if h.users.tenant != acme && h.posts.tenant != acme {
			t.Errorf("%s: the admin's tenant did not reach the service", path)
		}
	}
}

// every domain sentinel has one status, whatever route it came through
func TestErrors_Mapping(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err    error
		status int
		msg    string
	}{
		"not found":      {domain.ErrNotFound, http.StatusNotFound, "not found"},
		"unknown tenant": {fmt.Errorf("%w: relation does not exist", domain.ErrUnknownTenant), http.StatusNotFound, "tenant not found"},
		"conflict":       {domain.ErrConflict, http.StatusConflict, "already exists"},
		"invalid":        {fmt.Errorf("%w: author x does not exist", domain.ErrInvalid), http.StatusUnprocessableEntity, "invalid input: author x does not exist"},
		// whatever else went wrong is not the client's business
		"unexpected":      {errors.New("pq: password authentication failed"), http.StatusInternalServerError, "internal error"},
		"cancelled":       {context.Canceled, http.StatusInternalServerError, "internal error"},
		"unauthenticated": {domain.ErrUnauthenticated, http.StatusUnauthorized, "unauthenticated"},
		"forbidden":       {fmt.Errorf("%w: not yours", domain.ErrForbidden), http.StatusForbidden, "forbidden: not yours"},
	} {
		h := newHarness()
		h.users.err, h.posts.err, h.tenants.err = tc.err, tc.err, tc.err
		for _, r := range []request{
			{method: "GET", path: "/api/users", tenant: acmeText},
			{method: "GET", path: "/api/users/" + guyText, tenant: acmeText},
			{method: "DELETE", path: "/api/users/" + guyText, tenant: acmeText},
			{method: "POST", path: "/api/users", tenant: acmeText, body: `{"name":"Guy","email":"g@x.io","password":"hunter22"}`},
			{method: "GET", path: "/api/posts", tenant: acmeText},
			{method: "GET", path: "/api/posts/" + postText, tenant: acmeText},
			{method: "DELETE", path: "/api/posts/" + postText, tenant: acmeText},
			{method: "GET", path: "/api/users/" + guyText + "/posts", tenant: acmeText},
			{method: "POST", path: "/api/posts", tenant: acmeText, body: `{"author_id":"` + guyText + `","title":"x"}`},
			{method: "GET", path: "/api/tenants", admin: true},
			{method: "POST", path: "/api/tenants", admin: true, body: `{"slug":"acme","name":"Acme"}`},
		} {
			rec := h.do(t, r)
			if rec.Code != tc.status {
				t.Errorf("%s via %s %s:", name, r.method, r.path)
			}
			wantError(t, rec, tc.status, tc.msg)
		}
	}
}

func TestRouting_MethodsAndNotFound(t *testing.T) {
	t.Parallel()
	h := newHarness()

	for path, allow := range map[string]string{
		"/api/health":                      "GET",
		"/api/tenants":                     "GET, QUERY, POST",
		"/api/users":                       "GET, QUERY, POST",
		"/api/posts":                       "GET, QUERY, POST",
		"/api/users/" + guyText:            "GET, DELETE",
		"/api/posts/" + postText:           "GET, DELETE",
		"/api/users/" + guyText + "/posts": "GET",
	} {
		rec := h.do(t, request{method: "PATCH", path: path, tenant: acmeText})
		wantError(t, rec, http.StatusMethodNotAllowed, "method not allowed")
		if got := rec.Header().Get("Allow"); got != allow {
			t.Errorf("%s: Allow %q, want %q", path, got, allow)
		}
	}

	for _, path := range []string{
		"/", "/nope", "/api", "/api/", "/api/nope", "/api/users/", "/api/userss",
		"/api/users/abc", // not an id
		"/api/users/" + uuid.UUID(guyID).String(), // the rfc form is not the wire form
		"/api/users/" + guyText + "/nope",         // unknown sub-resource
		"/api/users/" + guyText + "/posts/" + postText,
		"/api/posts/" + postText + "/",
	} {
		wantError(t, h.do(t, request{method: "GET", path: path, tenant: acmeText}), http.StatusNotFound, "not found")
	}
	if h.users.id != (domain.ID{}) || h.posts.id != (domain.ID{}) {
		t.Error("an unroutable path reached a service")
	}
}

type panickingUsers struct{ *usersStub }

func (panickingUsers) List(context.Context) ([]domain.User, error) {
	panic("boom")
}

func (panickingUsers) Query(context.Context, domain.UserFilter) ([]domain.User, error) {
	panic("boom")
}

func TestPanic_BecomesPlain500(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.Users = panickingUsers{h.users}
	wantError(t, h.do(t, request{method: "GET", path: "/api/users", tenant: acmeText}), http.StatusInternalServerError, "internal error")
}

// the log names the route, not the id that came in, so calls of one route group
//
//nolint:paralleltest // captureLogs redirects the shared slog default, which every other test's logging would race
func TestRequestLog_NamesTheRoute(t *testing.T) {
	h := newHarness()
	buf := captureLogs(t)

	h.do(t, request{method: "GET", path: "/api/users/" + guyText, tenant: acmeText})
	h.do(t, request{method: "GET", path: "/api/users/" + guyText + "/posts", tenant: acmeText})
	h.do(t, request{method: "DELETE", path: "/api/users/" + guyText, tenant: acmeText})
	h.do(t, request{method: "GET", path: "/api/health"})
	// nothing matched: there is no route to name, so the path stands in
	h.do(t, request{method: "GET", path: "/nope"})
	h.do(t, request{method: "PATCH", path: "/api/users", tenant: acmeText})

	for _, want := range []string{
		"GET /api/users/{id} [200]",
		"GET /api/users/{id}/posts [200]",
		"DELETE /api/users/{id} [200]",
		"GET /api/health [200]",
		"GET /nope [404]",
		"PATCH /api/users [405]",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("no line %q in:\n%s", want, buf)
		}
	}
	if strings.Contains(buf.String(), guyText) {
		t.Errorf("the id reached the log, so lines do not group:\n%s", buf)
	}
}
