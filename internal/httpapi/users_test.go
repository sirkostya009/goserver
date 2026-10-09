package httpapi

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"goserver/domain"
)

var guy = domain.User{ID: guyID, Name: "Guy", Email: "guy@acme.io", CreatedAt: createdAt}

const guyJSON = `{"created_at":"` + createdAtText + `","email":"guy@acme.io","id":"` + guyText + `","name":"Guy"}`

func TestUsers_List(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.users.users = []domain.User{guy, guy}

	rec := h.do(t, request{method: "GET", path: "/api/users", tenant: acmeText})
	wantJSON(t, rec, `[`+guyJSON+`,`+guyJSON+`]`)
	if h.users.tenant != acme {
		t.Errorf("tenant bound from the header as %v", h.users.tenant)
	}

	h.users.users = []domain.User{}
	wantJSON(t, h.do(t, request{method: "GET", path: "/api/users", tenant: acmeText}), `[]`)
}

func TestUsers_Create(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.users.user = guy

	rec := h.do(t, request{method: "POST", path: "/api/users", tenant: acmeText,
		body: `{"name":"  Guy ","email":" Guy@Acme.IO ","password":" hunter22 "}`})
	wantJSON(t, rec, guyJSON)

	if h.users.tenant != acme {
		t.Errorf("tenant %v", h.users.tenant)
	}
	// trim and tolower run while decoding, the service never sees the raw input.
	// A password is taken byte for byte.
	if want := (domain.CreateUser{Name: "Guy", Email: "guy@acme.io", Password: " hunter22 "}); h.users.in != want {
		t.Errorf("service got %+v, want %+v", h.users.in, want)
	}
}

func TestUsers_Create_BadBodies(t *testing.T) {
	t.Parallel()
	//nolint:gosec // request bodies, not credentials
	for name, body := range map[string]string{
		"empty":          ``,
		"not json":       `nope`,
		"wrong type":     `{"name":1,"email":"g@x.io"}`,
		"missing name":   `{"email":"g@x.io"}`,
		"missing email":  `{"name":"Guy"}`,
		"blank name":     `{"name":"   ","email":"g@x.io"}`,
		"no at sign":     `{"name":"Guy","email":"nope"}`,
		"unknown key":    `{"name":"Guy","email":"g@x.io","admin":true}`,
		"duplicate key":  `{"name":"Guy","name":"Guy","email":"g@x.io"}`,
		"client sent id": `{"id":"` + guyText + `","name":"Guy","email":"g@x.io","password":"hunter22"}`,
		"no password":    `{"name":"Guy","email":"g@x.io"}`,
		"short password": `{"name":"Guy","email":"g@x.io","password":"1234567"}`,
		// counted in runes: 7 of them, 14 bytes
		"short in runes": `{"name":"Guy","email":"g@x.io","password":"яяяяяяя"}`,
		"long password":  `{"name":"Guy","email":"g@x.io","password":"` + strings.Repeat("x", 1025) + `"}`,
	} {
		h := newHarness()
		rec := h.do(t, request{method: "POST", path: "/api/users", tenant: acmeText, body: body})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d: %s", name, rec.Code, rec.Body)
		}
		if h.users.in != (domain.CreateUser{}) {
			t.Errorf("%s: reached the service as %+v", name, h.users.in)
		}
	}
}

// limits count characters, not bytes: 64 cyrillic letters are 128 bytes
func TestUsers_Create_NameLimitCountsRunes(t *testing.T) {
	t.Parallel()
	atLimit := ""
	var atLimitSb78 strings.Builder
	for range 64 {
		atLimitSb78.WriteString("я")
	}
	atLimit += atLimitSb78.String()
	h := newHarness()
	if rec := h.do(t, request{method: "POST", path: "/api/users", tenant: acmeText,
		body: `{"name":"` + atLimit + `","email":"a@x.io","password":"hunter22"}`}); rec.Code != http.StatusOK {
		t.Errorf("64 runes: %d %s", rec.Code, rec.Body)
	}
	if rec := h.do(t, request{method: "POST", path: "/api/users", tenant: acmeText,
		body: `{"name":"` + atLimit + `я","email":"a@x.io","password":"hunter22"}`}); rec.Code != http.StatusBadRequest {
		t.Errorf("65 runes: %d %s", rec.Code, rec.Body)
	}
}

func TestUsers_Get(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.users.user = guy

	wantJSON(t, h.do(t, request{method: "GET", path: "/api/users/" + guyText, tenant: acmeText}), guyJSON)
	if h.users.tenant != acme || h.users.id != guyID {
		t.Errorf("bound tenant %v id %v", h.users.tenant, h.users.id)
	}
}

func TestUsers_Delete(t *testing.T) {
	t.Parallel()
	h := newHarness()

	rec := h.do(t, request{method: "DELETE", path: "/api/users/" + guyText, tenant: acmeText})
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("want an empty 200, got %d %q", rec.Code, rec.Body)
	}
	if h.users.tenant != acme || h.users.id != guyID {
		t.Errorf("bound tenant %v id %v", h.users.tenant, h.users.id)
	}
}

// QUERY takes the filter as a json body, so a client sends shape a query string
// would not carry
func TestUsers_Query(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.users.users = []domain.User{guy}

	body := `{"ids":["` + guyText + `"],"name_like":"  Guy ","email_like":" ACME.IO ",` +
		`"created_after":"2026-01-01T00:00:00Z","sort":"name","desc":true,"page":{"limit":10,"offset":20}}`
	rec := h.do(t, request{method: "QUERY", path: "/api/users", tenant: acmeText, body: body})
	wantJSON(t, rec, `[`+guyJSON+`]`)

	got := h.users.filter
	if len(got.IDs) != 1 || got.IDs[0] != guyID {
		t.Errorf("ids: %v", got.IDs)
	}
	// the same mods the create body gets run here too
	if got.NameLike != "Guy" || got.EmailLike != "acme.io" {
		t.Errorf("text not trimmed and lowered: %q %q", got.NameLike, got.EmailLike)
	}
	if got.CreatedAfter == nil || got.CreatedAfter.Year() != 2026 || got.CreatedBefore != nil {
		t.Errorf("range: %v %v", got.CreatedAfter, got.CreatedBefore)
	}
	if got.Sort != domain.UserSortName || !got.Desc {
		t.Errorf("order: %q desc=%v", got.Sort, got.Desc)
	}
	if got.Page != (domain.Page{Limit: 10, Offset: 20}) {
		t.Errorf("page: %+v", got.Page)
	}

	// an empty filter is the whole first page
	h = newHarness()
	h.users.users = []domain.User{}
	wantJSON(t, h.do(t, request{method: "QUERY", path: "/api/users", tenant: acmeText, body: `{}`}), `[]`)
	if !reflect.DeepEqual(h.users.filter, domain.UserFilter{}) {
		t.Errorf("empty filter came through as %+v", h.users.filter)
	}
}

func TestUsers_Query_BadFilters(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"not json":        `nope`,
		"unknown key":     `{"nope":1}`,
		"unknown sort":    `{"sort":"password"}`,
		"negative limit":  `{"page":{"limit":-1}}`,
		"limit too big":   `{"page":{"limit":9000}}`,
		"negative offset": `{"page":{"offset":-5}}`,
		"too many ids":    `{"ids":[` + strings.TrimSuffix(strings.Repeat(`"`+guyText+`",`, 101), ",") + `]}`,
		"id not an id":    `{"ids":["nope"]}`,
		"wrong type":      `{"name_like":5}`,
	} {
		h := newHarness()
		rec := h.do(t, request{method: "QUERY", path: "/api/users", tenant: acmeText, body: body})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d: %s", name, rec.Code, rec.Body)
		}
		if !reflect.DeepEqual(h.users.filter, domain.UserFilter{}) {
			t.Errorf("%s: reached the service as %+v", name, h.users.filter)
		}
	}
}

// the filter is a body, and QUERY still needs a caller like every other read
func TestUsers_Query_NeedsCredential(t *testing.T) {
	t.Parallel()
	h := newHarness()
	wantError(t, h.do(t, request{method: "QUERY", path: "/api/users", body: `{}`}),
		http.StatusUnauthorized, "unauthenticated")
}
