package domain

import (
	"context"
	"testing"
	"uuid"
)

func TestTenant_Schema(t *testing.T) {
	t.Parallel()
	got := TenantSchema(TenantID(uuid.MustParse("01a0c081-9683-7dff-a267-f090f4ce1d17")))
	if got != "tenant_06gc10cmgduvv8j7u28f9jgt2s" {
		t.Errorf("got %q", got)
	}
	for i := len(tenantSchemaPrefix); i < len(got); i++ {
		if c := got[i]; (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			t.Errorf("%q needs quoting at %d", got, i)
		}
	}

	// v7 ids sort by creation and so must their schemas
	older, newer := TenantSchema(TenantID(uuid.NewV7())), TenantSchema(TenantID(uuid.NewV7()))
	if older >= newer {
		t.Errorf("order lost: %s then %s", older, newer)
	}
}

func TestTenant_Context(t *testing.T) {
	t.Parallel()
	id := TenantID(uuid.NewV7())
	ctx := WithTenant(context.Background(), id)

	tc, ok := TenantFrom(ctx)
	if !ok || tc.ID != id || tc.Schema != TenantSchema(id) {
		t.Errorf("got %+v %v", tc, ok)
	}
	if MustTenant(ctx) != tc {
		t.Error("MustTenant disagrees with TenantFrom")
	}

	if _, ok := TenantFrom(context.Background()); ok {
		t.Error("tenant found in an empty context")
	}
	defer func() {
		if recover() == nil {
			t.Error("MustTenant did not panic without a tenant")
		}
	}()
	MustTenant(context.Background())
}
