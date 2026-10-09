package domain

import (
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestID_Text(t *testing.T) {
	t.Parallel()
	id := TenantID(uuid.MustParse("01a0c081-9683-7dff-a267-f090f4ce1d17"))
	text := id.String()
	if text != "AaDAgZaDff-iZ_CQ9M4dFw" {
		t.Fatalf("got %q", text)
	}
	back, err := ParseTenantID(text)
	if err != nil || back != id {
		t.Errorf("round trip: %v %v", back, err)
	}

	// one spelling per tenant: the spare bits of the last symbol must be zero
	if _, err := ParseTenantID(text[:21] + "x"); err == nil {
		t.Error("non-canonical last symbol accepted")
	}
	for _, bad := range []string{"", "short", text + "A", uuid.UUID(id).String(), text[:21] + "="} {
		if _, err := ParseTenantID(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}

	// json goes through the generated codec and must agree with String
	tenant := Tenant{ID: id, Slug: "acme", Name: "Acme"}
	out, err := tenant.AppendJSON(nil)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := Tenant{}.DecodeFrom(out)
	if err != nil || decoded.ID != id {
		t.Errorf("json round trip: %s %v", out, err)
	}
	if want := `"id":"` + text + `"`; !contains(string(out), want) {
		t.Errorf("json %s lacks %s", out, want)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestID_Scan(t *testing.T) {
	t.Parallel()
	want := TenantID(uuid.MustParse("01a0c081-9683-7dff-a267-f090f4ce1d17"))
	raw := [16]byte(want)
	for _, src := range []any{"01a0c081-9683-7dff-a267-f090f4ce1d17", []byte("01a0c081-9683-7dff-a267-f090f4ce1d17"), raw[:], raw} {
		var got TenantID
		if err := got.Scan(src); err != nil || got != want {
			t.Errorf("%T: %v %v", src, got, err)
		}
	}
	var got ID
	if err := got.Scan(42); err == nil {
		t.Error("int accepted")
	}
}

// pgx hands a binary uuid to ScanUUID as is. Through Scan it would format it as
// text first, which allocates.
//
//nolint:paralleltest // AllocsPerRun counts the allocations of every goroutine
func TestID_ScanUUID_NoAllocs(t *testing.T) {
	want := TenantID(uuid.MustParse("01a0c081-9683-7dff-a267-f090f4ce1d17"))
	raw := [16]byte(want)
	m := pgtype.NewMap()
	var got TenantID
	if err := m.Scan(pgtype.UUIDOID, pgtype.BinaryFormatCode, raw[:], &got); err != nil || got != want {
		t.Fatalf("got %v %v", got, err)
	}
	if n := testing.AllocsPerRun(100, func() {
		_ = m.Scan(pgtype.UUIDOID, pgtype.BinaryFormatCode, raw[:], &got)
	}); n != 0 {
		t.Errorf("%v allocs per scan, want the native path", n)
	}
	if err := got.ScanUUID(pgtype.UUID{}); err == nil {
		t.Error("NULL accepted")
	}

	v, err := ID(want).UUIDValue()
	if err != nil || !v.Valid || v.Bytes != raw {
		t.Errorf("value: %v %v", v, err)
	}
}

func TestID_OnTheWire(t *testing.T) {
	t.Parallel()
	id := ID(uuid.MustParse("01a0c081-9683-7dff-a267-f090f4ce1d17"))
	out, err := Post{ID: id, AuthorID: id, Title: "x"}.AppendJSON(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"id":"AaDAgZaDff-iZ_CQ9M4dFw"`, `"author_id":"AaDAgZaDff-iZ_CQ9M4dFw"`} {
		if !contains(string(out), want) {
			t.Errorf("json %s lacks %s", out, want)
		}
	}
	in, _, err := CreatePost{}.DecodeFrom([]byte(`{"author_id":"AaDAgZaDff-iZ_CQ9M4dFw","title":"x"}`))
	if err != nil || in.AuthorID != id {
		t.Errorf("decode: %+v %v", in, err)
	}
	if _, _, err := (CreatePost{}).DecodeFrom([]byte(`{"author_id":"01a0c081-9683-7dff-a267-f090f4ce1d17","title":"x"}`)); err == nil {
		t.Error("rfc form accepted in a body")
	}
}
