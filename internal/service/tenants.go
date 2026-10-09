package service

import (
	"context"

	"goserver/domain"
	"goserver/internal/repository"
)

type TenantService interface {
	List(ctx context.Context) ([]domain.Tenant, error)
	Query(ctx context.Context, f domain.TenantFilter) ([]domain.Tenant, error)
	Create(ctx context.Context, in domain.CreateTenant) (domain.Tenant, error)
}

type tenantService struct {
	tenants repository.TenantRepository
}

func NewTenantService(tenants repository.TenantRepository) TenantService {
	return &tenantService{tenants: tenants}
}

// List is the whole list, which is the first page of no filter at all.
func (s *tenantService) List(ctx context.Context) ([]domain.Tenant, error) {
	return s.tenants.Query(ctx, domain.TenantFilter{})
}

func (s *tenantService) Query(ctx context.Context, f domain.TenantFilter) ([]domain.Tenant, error) {
	return s.tenants.Query(ctx, f)
}

func (s *tenantService) Create(ctx context.Context, in domain.CreateTenant) (domain.Tenant, error) {
	// registering is all the backend does: the tenant answers 404 until
	// cmd/migrate has created its schema
	return s.tenants.Insert(ctx, in)
}
