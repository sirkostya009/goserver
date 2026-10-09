package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"goserver/domain"
	"goserver/internal/auth"
	"goserver/internal/service"
)

func sessionToken() string {
	token, _ := auth.NewToken(auth.Session, acme)
	return token
}

func TestAuth_Login_SetsSessionCookie(t *testing.T) {
	t.Parallel()
	h := newHarness()
	token := sessionToken()
	h.auth.session = service.Session{Token: token, ExpiresAt: time.Now().Add(time.Hour), User: guy}

	rec := h.do(t, request{method: "POST", path: "/api/auth/login",
		body: `{"tenant":"acme","email":" Guy@Acme.IO ","password":" hunter22 "}`})
	wantJSON(t, rec, guyJSON)

	if want := (domain.Login{Tenant: "acme", Email: "guy@acme.io", Password: " hunter22 "}); h.auth.login != want {
		t.Errorf("service got %+v, want %+v", h.auth.login, want)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies %v", cookies)
	}
	c := cookies[0]
	if c.Name != "__Host-session" || c.Value != token || c.Path != "/" || !c.Secure || !c.HttpOnly ||
		c.SameSite != http.SameSiteLaxMode || c.Domain != "" {
		t.Errorf("cookie %+v", c)
	}
	// Max-Age is what the session has left, give or take the test's own time
	if c.MaxAge < 3590 || c.MaxAge > 3600 {
		t.Errorf("max age %d", c.MaxAge)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control %q", got)
	}
}

func TestAuth_Login_Failed_NoCookie(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.auth.err = errBadLogin

	rec := h.do(t, request{method: "POST", path: "/api/auth/login", body: `{"tenant":"acme","email":"g@x.io","password":"nope"}`})
	wantError(t, rec, http.StatusUnauthorized, "unauthenticated: wrong tenant, email or password")
	if len(rec.Result().Cookies()) != 0 {
		t.Error("a failed login set a cookie")
	}
}

var errBadLogin = fmt.Errorf("%w: wrong tenant, email or password", domain.ErrUnauthenticated)

func TestAuth_Login_BadBodies(t *testing.T) {
	t.Parallel()
	//nolint:gosec // request bodies, not credentials
	for name, body := range map[string]string{
		"no tenant":     `{"email":"g@x.io","password":"x"}`,
		"bad slug":      `{"tenant":"Acme!","email":"g@x.io","password":"x"}`,
		"no email":      `{"tenant":"acme","password":"x"}`,
		"no password":   `{"tenant":"acme","email":"g@x.io"}`,
		"long password": `{"tenant":"acme","email":"g@x.io","password":"` + strings.Repeat("x", 1025) + `"}`,
	} {
		h := newHarness()
		rec := h.do(t, request{method: "POST", path: "/api/auth/login", body: body})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d: %s", name, rec.Code, rec.Body)
		}
		if h.auth.login != (domain.Login{}) {
			t.Errorf("%s: reached the service", name)
		}
	}
}

// a cookie that opens nothing leaves the request anonymous, never failed, so it
// cannot keep its holder from logging in again
func TestAuth_Login_StaleCookie_StillLogsIn(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.auth.reject = true
	h.auth.session = service.Session{Token: sessionToken(), ExpiresAt: time.Now().Add(time.Hour), User: guy}

	rec := h.do(t, request{method: "POST", path: "/api/auth/login", body: `{"tenant":"acme","email":"g@x.io","password":"x"}`,
		header: map[string]string{"Cookie": sessionCookie + "=" + sessionToken()}})
	wantJSON(t, rec, guyJSON)
}

func TestAuth_SessionCookie_Authenticates(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.users.users = []domain.User{}
	wantJSON(t, h.do(t, request{method: "GET", path: "/api/users",
		header: map[string]string{"Cookie": sessionCookie + "=" + sessionToken()}}), `[]`)
	if h.users.tenant != acme {
		t.Errorf("tenant %v, want the token's", h.users.tenant)
	}
}

// a session stays in the browser's jar and a key stays out of it, so a key is
// never ambient and a session never travels as a header
func TestAuth_CredentialsOnlyWhereTheyBelong(t *testing.T) {
	t.Parallel()
	h := newHarness()
	key, _ := auth.NewToken(auth.APIKey, acme)
	for name, header := range map[string]map[string]string{
		"session as bearer": {"Authorization": "Bearer " + sessionToken()},
		"key as cookie":     {"Cookie": sessionCookie + "=" + key},
		"admin as cookie":   {"Cookie": sessionCookie + "=" + adminToken},
	} {
		rec := h.do(t, request{method: "GET", path: "/api/users", header: header})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}
	if h.users.tenant != (domain.TenantID{}) {
		t.Error("a misplaced credential reached a service")
	}
}

func TestAuth_Logout_ClearsCookie(t *testing.T) {
	t.Parallel()
	h := newHarness()
	rec := h.do(t, request{method: "POST", path: "/api/auth/logout",
		header: map[string]string{"Cookie": sessionCookie + "=" + sessionToken()}})
	if rec.Code != http.StatusOK || !h.auth.loggedOut {
		t.Fatalf("status %d, logged out %t", rec.Code, h.auth.loggedOut)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookie || cookies[0].MaxAge >= 0 || cookies[0].Value != "" {
		t.Errorf("cookies %+v, want the session one cleared", cookies)
	}

	h = newHarness()
	h.auth.err = domain.ErrNotFound // deleted meanwhile
	if rec := h.do(t, request{method: "POST", path: "/api/auth/logout", tenant: acmeText}); rec.Code != http.StatusOK {
		t.Errorf("an already gone session: status %d", rec.Code)
	}
}

func TestAuth_Logout_Anonymous(t *testing.T) {
	t.Parallel()
	h := newHarness()
	rec := h.do(t, request{method: "POST", path: "/api/auth/logout"})
	wantError(t, rec, http.StatusUnauthorized, "unauthenticated")
	if len(rec.Result().Cookies()) != 0 {
		t.Error("cleared a cookie for nobody")
	}
}

func TestAuth_Me(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.auth.user = guy
	wantJSON(t, h.do(t, request{method: "GET", path: "/api/auth/me", tenant: acmeText}), guyJSON)
	wantError(t, h.do(t, request{method: "GET", path: "/api/auth/me"}), http.StatusUnauthorized, "unauthenticated")
}

// a browser says where a request came from, and a write from another site is
// refused before anything reads the credential it carries along
func TestServer_CrossOriginWrites_Refused(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.auth.session = service.Session{Token: sessionToken(), ExpiresAt: time.Now().Add(time.Hour), User: guy}
	login := `{"tenant":"acme","email":"g@x.io","password":"x"}`

	for _, method := range []string{"POST", "DELETE", "QUERY"} {
		rec := h.do(t, request{method: method, path: "/api/users", tenant: acmeText, body: `{}`,
			header: map[string]string{"Sec-Fetch-Site": "cross-site"}})
		wantError(t, rec, http.StatusForbidden, "cross-origin request refused")
	}
	rec := h.do(t, request{method: "POST", path: "/api/auth/login", body: login,
		header: map[string]string{"Origin": "https://evil.example"}})
	wantError(t, rec, http.StatusForbidden, "cross-origin request refused")
	if h.auth.login != (domain.Login{}) || h.users.tenant != (domain.TenantID{}) {
		t.Error("a cross-origin write reached a service")
	}

	// reads, the same origin, and clients that are no browser all pass
	h.users.users = []domain.User{}
	wantJSON(t, h.do(t, request{method: "GET", path: "/api/users", tenant: acmeText,
		header: map[string]string{"Sec-Fetch-Site": "cross-site"}}), `[]`)
	wantJSON(t, h.do(t, request{method: "POST", path: "/api/auth/login", body: login,
		header: map[string]string{"Sec-Fetch-Site": "same-origin"}}), guyJSON)
	wantJSON(t, h.do(t, request{method: "POST", path: "/api/auth/login", body: login}), guyJSON)
}

var guyKey = domain.APIKey{ID: keyID, UserID: guyID, Name: "ci", CreatedAt: createdAt}

const guyKeyJSON = `{"created_at":"` + createdAtText + `","expires_at":null,"id":"` + keyText + `","name":"ci","user_id":"` + guyText + `"}`

func TestKeys_Create(t *testing.T) {
	t.Parallel()
	h := newHarness()
	token, _ := auth.NewToken(auth.APIKey, acme)
	h.keys.key = domain.NewAPIKey{Key: guyKey, Token: token}

	expires := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	rec := h.do(t, request{method: "POST", path: "/api/keys", body: `{"name":" ci ","expires_at":"2030-01-02T03:04:05Z"}`,
		header: map[string]string{"Cookie": sessionCookie + "=" + sessionToken()}})
	wantJSON(t, rec, `{"key":`+guyKeyJSON+`,"token":"`+token+`"}`)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control %q", got)
	}
	if h.keys.in.Name != "ci" || h.keys.in.ExpiresAt == nil || !h.keys.in.ExpiresAt.Equal(expires) {
		t.Errorf("service got %+v", h.keys.in)
	}
	if h.keys.tenant != acme || h.keys.principal.Via != domain.CredentialSession || h.keys.principal.UserID != guyID {
		t.Errorf("caller %+v in %v", h.keys.principal, h.keys.tenant)
	}
}

func TestKeys_Create_BadBodies(t *testing.T) {
	t.Parallel()
	//nolint:gosec // request bodies, not credentials
	for name, body := range map[string]string{
		"no name":      `{}`,
		"blank name":   `{"name":"  "}`,
		"bad expiry":   `{"name":"ci","expires_at":"tomorrow"}`,
		"client token": `{"name":"ci","token":"gsk_x"}`,
	} {
		h := newHarness()
		rec := h.do(t, request{method: "POST", path: "/api/keys", tenant: acmeText, body: body})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d: %s", name, rec.Code, rec.Body)
		}
		if h.keys.in != (domain.CreateAPIKey{}) {
			t.Errorf("%s: reached the service", name)
		}
	}
}

func TestKeys_ListAndRevoke(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.keys.keys = []domain.APIKey{guyKey}
	wantJSON(t, h.do(t, request{method: "GET", path: "/api/keys", tenant: acmeText}), `[`+guyKeyJSON+`]`)

	rec := h.do(t, request{method: "DELETE", path: "/api/keys/" + keyText, tenant: acmeText})
	if rec.Code != http.StatusOK || h.keys.id != keyID {
		t.Errorf("status %d, revoked %v", rec.Code, h.keys.id)
	}

	h.keys.err = domain.ErrForbidden
	wantError(t, h.do(t, request{method: "DELETE", path: "/api/keys/" + keyText, tenant: acmeText}), http.StatusForbidden, "forbidden")
}

func TestAdminToken_ComparedWhole(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.tenants.tenants = []domain.Tenant{}
	for i, token := range []string{adminToken[:len(adminToken)-1], adminToken + " ", strings.ToUpper(adminToken), ""} {
		rec := h.do(t, request{method: "GET", path: "/api/tenants", header: map[string]string{"Authorization": "Bearer " + token}})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("variant %s: status %d", strconv.Itoa(i), rec.Code)
		}
	}
	wantJSON(t, h.do(t, request{method: "GET", path: "/api/tenants", admin: true}), `[]`)

	// a server with no admin token has no admin, whatever is presented
	h.adminHash = nil
	if rec := h.do(t, request{method: "GET", path: "/api/tenants", admin: true}); rec.Code != http.StatusUnauthorized {
		t.Errorf("no admin token configured: status %d", rec.Code)
	}
}
