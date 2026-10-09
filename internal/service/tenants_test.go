package service_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"goserver/domain"
)

func TestTenants_CreateAndList(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()

	acme, err := f.Services.Tenants.Create(ctx, domain.CreateTenant{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	if acme.Slug != "acme" || acme.Name != "Acme" {
		t.Errorf("input not carried over: %+v", acme)
	}
	if acme.ID == (domain.TenantID{}) || acme.CreatedAt.IsZero() {
		t.Errorf("no generated id or timestamp: %+v", acme)
	}

	if _, err := f.Services.Tenants.Create(ctx, domain.CreateTenant{Slug: "globex", Name: "Globex"}); err != nil {
		t.Fatal(err)
	}

	all, err := f.Services.Tenants.List(ctx)
	if err != nil || len(all) != 2 || all[0] != acme || all[1].Slug != "globex" {
		t.Errorf("list is not in slug order: %v %v", all, err)
	}
}

func TestTenants_Create_DuplicateSlug(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.Services.Tenants.Create(ctx, domain.CreateTenant{Slug: "acme", Name: "Acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Services.Tenants.Create(ctx, domain.CreateTenant{Slug: "acme", Name: "Again"}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("duplicate slug: %v", err)
	}
}

// registering a tenant does not create its schema: cmd/migrate does
func TestTenants_Create_LeavesSchemaToMigrate(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()

	fresh, err := f.Services.Tenants.Create(ctx, domain.CreateTenant{Slug: "fresh", Name: "Fresh"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Services.Users.List(domain.WithTenant(ctx, fresh.ID)); !errors.Is(err, domain.ErrUnknownTenant) {
		t.Errorf("the schema exists already: %v", err)
	}
}

func TestTenants_Query_Filters(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	id := map[string]domain.TenantID{}
	for _, slug := range []string{"acme", "acme-eu", "globex"} {
		tenant, err := f.Services.Tenants.Create(ctx, domain.CreateTenant{Slug: slug, Name: strings.ToUpper(slug)})
		if err != nil {
			t.Fatal(err)
		}
		id[slug] = tenant.ID
	}

	slugs := func(ts []domain.Tenant) []string {
		out := make([]string, len(ts))
		for i, tenant := range ts {
			out[i] = tenant.Slug
		}
		return out
	}

	for name, tc := range map[string]struct {
		filter domain.TenantFilter
		want   []string
	}{
		"nothing set":     {domain.TenantFilter{}, []string{"acme", "acme-eu", "globex"}},
		"slug substring":  {domain.TenantFilter{SlugLike: "acme"}, []string{"acme", "acme-eu"}},
		"name substring":  {domain.TenantFilter{NameLike: "GLOB"}, []string{"globex"}},
		"reverse order":   {domain.TenantFilter{Desc: true}, []string{"globex", "acme-eu", "acme"}},
		"one per page":    {domain.TenantFilter{Page: domain.Page{Limit: 1}}, []string{"acme"}},
		"second page":     {domain.TenantFilter{Page: domain.Page{Limit: 1, Offset: 1}}, []string{"acme-eu"}},
		"dash is literal": {domain.TenantFilter{SlugLike: "acme-"}, []string{"acme-eu"}},
		// the ids bind as one uuid[]
		"by ids": {domain.TenantFilter{IDs: []domain.TenantID{id["globex"], id["acme"]}}, []string{"acme", "globex"}},
	} {
		got, err := f.Services.Tenants.Query(ctx, tc.filter)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !slices.Equal(slugs(got), tc.want) {
			t.Errorf("%s: got %v, want %v", name, slugs(got), tc.want)
		}
	}
}
