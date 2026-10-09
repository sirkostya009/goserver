package httpapi

import (
	"context"

	"goserver/domain"
	"goserver/internal/service"
)

// TenantsApi manages the shared registry, which only the operator may.
//
//rr:pre @requireAdmin
type TenantsApi struct {
	Tenants service.TenantService
}

//rr:route GET /api/tenants
func (a *TenantsApi) List(ctx context.Context) ([]domain.Tenant, error) {
	return a.Tenants.List(ctx)
}

//rr:route QUERY /api/tenants
func (a *TenantsApi) Query(ctx context.Context, body domain.TenantFilter) ([]domain.Tenant, error) {
	return a.Tenants.Query(ctx, body)
}

//rr:route POST /api/tenants
func (a *TenantsApi) Create(ctx context.Context, body domain.CreateTenant) (domain.Tenant, error) {
	return a.Tenants.Create(ctx, body)
}
