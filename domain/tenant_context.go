package domain

import (
	"context"
	"encoding/base32"
)

// TenantContext is the tenant a request acts for. The http layer puts it in the
// request's context once, and everything below reads it from there instead of
// taking a tenant argument.
type TenantContext struct {
	ID TenantID
	// Schema holds the tenant's tables. It is derived from ID here, once per
	// request, rather than once per query.
	Schema string
}

type tenantContextKey struct{}

func WithTenant(ctx context.Context, id TenantID) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, &TenantContext{ID: id, Schema: TenantSchema(id)})
}

func TenantFrom(ctx context.Context) (*TenantContext, bool) {
	tc, ok := ctx.Value(tenantContextKey{}).(*TenantContext)
	return tc, ok
}

// MustTenant is for code that only ever runs for a tenant. A context without
// one there is a wiring bug, not a request error, so it panics.
func MustTenant(ctx context.Context) *TenantContext {
	tc, ok := TenantFrom(ctx)
	if !ok {
		panic("domain: no tenant in context")
	}
	return tc
}

const tenantSchemaPrefix = "tenant_"

// lowercase base32hex: every symbol is legal in an unquoted postgres
// identifier, and it keeps byte order, so schemas of v7 ids sort by creation.
var schemaEncoding = base32.NewEncoding("0123456789abcdefghijklmnopqrstuv").WithPadding(base32.NoPadding)

// TenantSchema names the schema holding a tenant's tables. It is a pure
// function of the id, so finding a tenant's data costs no lookup.
func TenantSchema(id TenantID) string {
	var b [len(tenantSchemaPrefix) + 26]byte
	copy(b[:], tenantSchemaPrefix)
	schemaEncoding.Encode(b[len(tenantSchemaPrefix):], id[:])
	return string(b[:])
}
