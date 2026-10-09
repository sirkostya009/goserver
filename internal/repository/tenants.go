package repository

import (
	"context"

	"goserver/domain"
	"goserver/internal/migrations"
)

type TenantRepository interface {
	// Query lists the tenants matching f, page and order included.
	Query(ctx context.Context, f domain.TenantFilter) ([]domain.Tenant, error)
	BySlug(ctx context.Context, slug string) (domain.Tenant, error)
	// Insert writes the row and returns it as the database stored it.
	Insert(ctx context.Context, in domain.CreateTenant) (domain.Tenant, error)
}

// the table domain.Tenant names, see usersTable
const tenantsTable = migrations.SharedSchema + ".tenants"

type tenantRepository struct{ base }

func (r *tenantRepository) Query(ctx context.Context, f domain.TenantFilter) ([]domain.Tenant, error) {
	tenants, err := r.handle(ctx).Select[domain.Tenant]().Apply(f.Q).Slice(ctx)
	return tenants, mapErr(err)
}

func (r *tenantRepository) BySlug(ctx context.Context, slug string) (domain.Tenant, error) {
	db := r.handle(ctx)
	t, err := db.Select[domain.Tenant]().Where("slug = ?", slug).One(ctx)
	return t, mapErr(err)
}

func (r *tenantRepository) Insert(ctx context.Context, in domain.CreateTenant) (domain.Tenant, error) {
	db := r.handle(ctx)
	t, err := db.Insert[domain.CreateTenant]().Table(tenantsTable).Values(&in).OneAs[domain.Tenant](ctx)
	return t, mapErr(err)
}
