package httpapi

import (
	"net/http"
	"testing"

	"goserver/domain"
)

var acmeTenant = domain.Tenant{ID: acme, Slug: "acme", Name: "Acme", CreatedAt: createdAt}

const acmeJSON = `{"created_at":"` + createdAtText + `","id":"` + acmeText + `","name":"Acme","slug":"acme"}`

func TestTenants_List(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.tenants.tenants = []domain.Tenant{acmeTenant}

	wantJSON(t, h.do(t, request{method: "GET", path: "/api/tenants", admin: true}), `[`+acmeJSON+`]`)
}

func TestTenants_Create(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.tenants.tenant = acmeTenant

	rec := h.do(t, request{method: "POST", path: "/api/tenants", admin: true, body: `{"slug":"acme","name":"  Acme "}`})
	wantJSON(t, rec, acmeJSON)
	if want := (domain.CreateTenant{Slug: "acme", Name: "Acme"}); h.tenants.in != want {
		t.Errorf("service got %+v, want %+v", h.tenants.in, want)
	}
}

func TestTenants_Create_BadBodies(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"missing slug":  `{"name":"Acme"}`,
		"missing name":  `{"slug":"acme"}`,
		"upper case":    `{"slug":"Acme","name":"Acme"}`,
		"space in slug": `{"slug":"ac me","name":"Acme"}`,
		"empty slug":    `{"slug":"","name":"Acme"}`,
		"long slug":     `{"slug":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":"Acme"}`,
		"client id":     `{"id":"` + acmeText + `","slug":"acme","name":"Acme"}`,
	} {
		h := newHarness()
		rec := h.do(t, request{method: "POST", path: "/api/tenants", admin: true, body: body})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d: %s", name, rec.Code, rec.Body)
		}
		if h.tenants.in != (domain.CreateTenant{}) {
			t.Errorf("%s: reached the service as %+v", name, h.tenants.in)
		}
	}
}

// the registry is the operator's: a tenant's own user is somebody, and not them
func TestTenants_AdminOnly(t *testing.T) {
	t.Parallel()
	h := newHarness()
	for _, r := range []request{
		{method: "GET", path: "/api/tenants"},
		{method: "POST", path: "/api/tenants", body: `{"slug":"acme","name":"Acme"}`},
	} {
		rec := h.do(t, r)
		wantError(t, rec, http.StatusUnauthorized, "unauthenticated")
		if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
			t.Errorf("WWW-Authenticate %q", got)
		}
		r.tenant = acmeText
		wantError(t, h.do(t, r), http.StatusForbidden, "forbidden")
		r.tenant, r.header = "", map[string]string{"Authorization": "Bearer " + adminToken + "x"}
		wantError(t, h.do(t, r), http.StatusUnauthorized, "unauthenticated")
	}
	if h.tenants.in != (domain.CreateTenant{}) {
		t.Error("a non-admin reached the service")
	}
}
