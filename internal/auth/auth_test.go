package auth

import (
	"strings"
	"testing"
	"uuid"

	"goserver/domain"
)

var acme = domain.TenantID(uuid.MustParse("01a0c081-9683-7dff-a267-f090f4ce1d17"))

func TestToken_RoundTrip(t *testing.T) {
	t.Parallel()
	for _, kind := range []Kind{Session, APIKey} {
		token, hash := NewToken(kind, acme)
		if len(token) != 68 || !strings.HasPrefix(token, string(kind)) {
			t.Errorf("token %q", token)
		}
		k, tenant, h, ok := ParseToken(token)
		if !ok || k != kind || tenant != acme || h != hash {
			t.Errorf("parse %q: %s %v %t", token, k, tenant, ok)
		}
	}
	a, _ := NewToken(Session, acme)
	b, _ := NewToken(Session, acme)
	if a == b {
		t.Error("two tokens are equal")
	}
}

func TestToken_Malformed(t *testing.T) {
	t.Parallel()
	good, _ := NewToken(APIKey, acme)
	for _, token := range []string{
		"",
		good[:67],
		good + "A",
		"gsx_" + good[4:],
		good[:10] + "!" + good[11:],
	} {
		if _, _, _, ok := ParseToken(token); ok && token != good {
			t.Errorf("%q accepted", token)
		}
	}
}

// the tenant is in the clear, so it is also under the hash: moving a secret to
// another tenant yields a token nobody issued
func TestToken_TenantBound(t *testing.T) {
	t.Parallel()
	token, hash := NewToken(Session, acme)
	other := domain.TenantID(uuid.MustParse("01a0c081-9683-7dff-a267-f090f4ce1d18"))
	forged, _ := NewToken(Session, other)
	forged = forged[:4+22] + token[4+22:] // the other tenant's id, this secret
	if _, _, h, ok := ParseToken(forged); ok && h == hash {
		t.Error("same hash under another tenant")
	}
}

// a hash is its column's 32 bytes, and anything else is an error rather than a
// silent truncation or a panic
func TestHash_ScanValue(t *testing.T) {
	t.Parallel()
	_, h := NewToken(Session, acme)
	v, err := h.Value()
	if err != nil {
		t.Fatal(err)
	}
	var back Hash
	if err := back.Scan(v); err != nil || back != h {
		t.Errorf("round trip: %v %v", back, err)
	}
	for _, src := range []any{h[:31], append(h[:], 0), "x", nil} {
		if err := back.Scan(src); err == nil {
			t.Errorf("scanned %#v", src)
		}
	}
}
