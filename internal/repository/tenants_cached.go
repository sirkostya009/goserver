package repository

import (
	"context"

	"goserver/domain"
	"goserver/internal/cache"
)

type cachedTenantRepository struct {
	repo    TenantRepository
	clock   *cache.Clock
	tenants *cache.Map[string, domain.Tenant]
}

func (r *cachedTenantRepository) Query(ctx context.Context, f domain.TenantFilter) ([]domain.Tenant, error) {
	return r.repo.Query(ctx, f)
}

func (r *cachedTenantRepository) BySlug(ctx context.Context, slug string) (domain.Tenant, error) {
	return cached(ctx, r.clock, r.tenants, tTenants, slug, func() (domain.Tenant, error) {
		return r.repo.BySlug(ctx, slug)
	})
}

func (r *cachedTenantRepository) Insert(ctx context.Context, in domain.CreateTenant) (domain.Tenant, error) {
	t, err := r.repo.Insert(ctx, in)
	wrote(ctx, tTenants, nil)
	return t, err
}
