package repository

import (
	"context"

	"goserver/domain"
)

type cachedTenantRepository struct {
	TenantRepository

	cache *tableCache[string, domain.Tenant]
}

func (r *cachedTenantRepository) BySlug(ctx context.Context, slug string) (domain.Tenant, error) {
	return r.cache.get(ctx, slug, func() (domain.Tenant, error) { return r.TenantRepository.BySlug(ctx, slug) })
}

func (r *cachedTenantRepository) Insert(ctx context.Context, in domain.CreateTenant) (domain.Tenant, error) {
	t, err := r.TenantRepository.Insert(ctx, in)
	r.cache.wrote(ctx)
	return t, err
}
