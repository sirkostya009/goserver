package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"goserver/domain"
)

var (
	guyID, _  = domain.ParseID("AZmQDbF0dYKmUv1bTGYBAw")
	acme, _   = domain.ParseTenantID("AZmQDbF0dYKmUv1bTGYAAA")
	createdAt = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	guy       = domain.User{ID: guyID, Name: "Guy", Email: "guy@acme.io", CreatedAt: createdAt}
	guyJSON   = `{"created_at":"2026-10-06T12:00:00Z","email":"guy@acme.io","id":"AZmQDbF0dYKmUv1bTGYBAw","name":"Guy"}`
)

// seen is one request as the server read it.
type seen struct {
	method, path, auth, tenant, contentType, body string
}

type reply struct {
	status int
	body   string
}

// serve answers every request with r and records what it read.
func serve(t *testing.T, r reply) (*seen, string) {
	t.Helper()
	got := &seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		*got = seen{
			method:      req.Method,
			path:        req.URL.EscapedPath(),
			auth:        req.Header.Get("Authorization"),
			tenant:      req.Header.Get("X-Tenant-Id"),
			contentType: req.Header.Get("Content-Type"),
			body:        string(b),
		}
		w.WriteHeader(r.status)
		_, _ = io.WriteString(w, r.body)
	}))
	t.Cleanup(srv.Close)
	return got, srv.URL
}

func TestClient_Get_PathAndCredentials(t *testing.T) {
	t.Parallel()
	got, url := serve(t, reply{http.StatusOK, guyJSON})
	c := New(url+"/", Options{Token: "gsk_abc", Tenant: acme})

	u, err := c.Users.Get(context.Background(), guyID)
	if err != nil {
		t.Fatal(err)
	}
	if u != guy {
		t.Errorf("got %+v, want %+v", u, guy)
	}
	want := seen{method: "GET", path: "/api/users/AZmQDbF0dYKmUv1bTGYBAw", auth: "Bearer gsk_abc", tenant: acme.String()}
	if *got != want {
		t.Errorf("server read %+v, want %+v", *got, want)
	}
}

func TestClient_Anonymous_SendsNoCredentials(t *testing.T) {
	t.Parallel()
	got, url := serve(t, reply{http.StatusOK, `{"status":"ok"}`})

	h, err := New(url, Options{}).Health.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "ok" {
		t.Errorf("status %q", h.Status)
	}
	if got.auth != "" || got.tenant != "" {
		t.Errorf("anonymous request sent auth %q, tenant %q", got.auth, got.tenant)
	}
}

func TestClient_Query_SendsBody(t *testing.T) {
	t.Parallel()
	got, url := serve(t, reply{http.StatusOK, `[` + guyJSON + `,` + guyJSON + `]`})

	users, err := New(url, Options{}).Users.Query(context.Background(), domain.UserFilter{
		NameLike: "gu", Sort: domain.UserSortName, Page: domain.Page{Limit: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0] != guy || users[1] != guy {
		t.Errorf("got %+v", users)
	}
	want := seen{
		method: "QUERY", path: "/api/users", contentType: "application/json",
		body: `{"desc":false,"name_like":"gu","page":{"limit":10,"offset":0},"sort":"name"}`,
	}
	if *got != want {
		t.Errorf("server read %+v, want %+v", *got, want)
	}
}

func TestClient_List_Empty(t *testing.T) {
	t.Parallel()
	_, url := serve(t, reply{http.StatusOK, `[]`})

	users, err := New(url, Options{}).Users.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if users == nil || len(users) != 0 {
		t.Errorf("got %#v, want an empty slice", users)
	}
}

func TestClient_Delete_NoBody(t *testing.T) {
	t.Parallel()
	got, url := serve(t, reply{http.StatusOK, ""})

	if err := New(url, Options{}).Posts.Delete(context.Background(), guyID); err != nil {
		t.Fatal(err)
	}
	if got.method != "DELETE" || got.path != "/api/posts/AZmQDbF0dYKmUv1bTGYBAw" || got.body != "" {
		t.Errorf("server read %+v", *got)
	}
}

func TestClient_NestedPath(t *testing.T) {
	t.Parallel()
	got, url := serve(t, reply{http.StatusOK, `[]`})

	if _, err := New(url, Options{}).Posts.ListByAuthor(context.Background(), guyID); err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/users/AZmQDbF0dYKmUv1bTGYBAw/posts" {
		t.Errorf("path %q", got.path)
	}
}

func TestClient_WithTenant_LeavesOriginal(t *testing.T) {
	t.Parallel()
	got, url := serve(t, reply{http.StatusOK, `[]`})
	admin := New(url, Options{Token: "admin"})

	if _, err := admin.WithTenant(acme).Users.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got.tenant != acme.String() || got.auth != "Bearer admin" {
		t.Errorf("server read %+v", *got)
	}
	if _, err := admin.Tenants.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got.tenant != "" {
		t.Errorf("original client sent tenant %q", got.tenant)
	}
}

func TestClient_TrailingData_Rejected(t *testing.T) {
	t.Parallel()
	_, url := serve(t, reply{http.StatusOK, guyJSON + `{}`})

	if _, err := New(url, Options{}).Auth.Me(context.Background()); err == nil {
		t.Error("decoded a body with trailing data")
	}
}

func TestClient_Error_StatusAndMessage(t *testing.T) {
	t.Parallel()
	_, url := serve(t, reply{http.StatusTeapot, "short and stout\n"})

	_, err := New(url, Options{}).Auth.Me(context.Background())
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("got %v, want *Error", err)
	}
	if e.Status != http.StatusTeapot || e.Message != "short and stout" {
		t.Errorf("got %+v", e)
	}
	if err.Error() != "418 short and stout" {
		t.Errorf("message %q", err.Error())
	}
}

func TestError_Is_DomainSentinels(t *testing.T) {
	t.Parallel()
	sentinels := []error{
		domain.ErrNotFound, domain.ErrConflict, domain.ErrInvalid, domain.ErrMalformed, domain.ErrUnknownTenant,
		domain.ErrUnauthenticated, domain.ErrForbidden,
	}
	for _, tc := range []struct {
		status int
		msg    string
		want   []error
	}{
		{http.StatusNotFound, "not found", []error{domain.ErrNotFound}},
		{http.StatusNotFound, "tenant not found", []error{domain.ErrUnknownTenant}},
		{http.StatusConflict, "already exists: slug", []error{domain.ErrConflict}},
		{http.StatusUnprocessableEntity, "invalid input", []error{domain.ErrInvalid}},
		// a malformed body is invalid input too
		{http.StatusBadRequest, "name: required", []error{domain.ErrInvalid, domain.ErrMalformed}},
		{http.StatusUnauthorized, "unauthenticated", []error{domain.ErrUnauthenticated}},
		{http.StatusForbidden, "forbidden", []error{domain.ErrForbidden}},
		{http.StatusInternalServerError, "internal error", nil},
	} {
		err := error(&Error{Status: tc.status, Message: tc.msg})
		for _, s := range sentinels {
			if got := errors.Is(err, s); got != slices.Contains(tc.want, s) {
				t.Errorf("%d %q: errors.Is(%v) = %v", tc.status, tc.msg, s, got)
			}
		}
	}
}

func TestClient_ErrorBody_Trimmed(t *testing.T) {
	t.Parallel()
	_, url := serve(t, reply{http.StatusNotFound, "tenant not found\n"})

	_, err := New(url, Options{}).Users.List(context.Background())
	if !errors.Is(err, domain.ErrUnknownTenant) {
		t.Errorf("got %v", err)
	}
	if strings.HasSuffix(err.Error(), "\n") {
		t.Errorf("message kept its newline: %q", err.Error())
	}
}
