package repository

import (
	"context"

	"github.com/sirkostya009/barm"

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

type bySlug struct {
	Slug string `barm:"slug"`
}

var tenantBySlug = qb.Select[domain.Tenant]().Where("slug = ?", barm.Param("slug")).Template[bySlug]()

func (r *tenantRepository) BySlug(ctx context.Context, slug string) (domain.Tenant, error) {
	t, err := tenantBySlug.Via(r.handle(ctx)).One(ctx, bySlug{slug})
	return t, mapErr(err)
}

var tenantInsert = qb.Insert[domain.CreateTenant]().Table(tenantsTable).TemplateAs[domain.CreateTenant, domain.Tenant]()

func (r *tenantRepository) Insert(ctx context.Context, in domain.CreateTenant) (domain.Tenant, error) {
	t, err := tenantInsert.Via(r.handle(ctx)).One(ctx, in)
	return t, mapErr(err)
}
