// Command migrate applies the schema migrations the backend refuses to.
//
//	migrate shared            the shared schema
//	migrate tenants           the schema of every registered tenant
//	migrate tenant <slug>...  the schemas of the named tenants
//	migrate all               shared, then tenants
//	migrate status            versions, nothing applied
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sirkostya009/barm"

	"goserver/domain"
	"goserver/internal/config"
	"goserver/internal/migrations"
	"goserver/internal/repository"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx := context.Background()

	cfg := config.Parse()
	db, err := repository.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fail(err)
	}
	defer db.Close()
	m := migrator{db: db, registry: repository.New(db).Tenants}

	switch cmd, args := os.Args[1], os.Args[2:]; cmd {
	case "shared":
		err = m.shared(ctx)
	case "tenants":
		err = m.tenants(ctx, nil)
	case "tenant":
		if len(args) == 0 {
			usage()
		}
		err = m.tenants(ctx, args)
	case "all":
		if err = m.shared(ctx); err == nil {
			err = m.tenants(ctx, nil)
		}
	case "status":
		err = m.status(ctx)
	default:
		usage()
	}
	if err != nil {
		db.Close()
		fail(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: migrate shared | tenants | tenant <slug>... | all | status")
	os.Exit(2)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "migrate:", err)
	os.Exit(1)
}

type migrator struct {
	db       *barm.DB
	registry repository.TenantRepository
}

var sharedTarget = target{schema: migrations.SharedSchema, label: migrations.SharedSchema}

func (m migrator) shared(ctx context.Context) error {
	return m.apply(ctx, migrations.Shared, sharedTarget)
}

// tenants migrates the named tenants, or every registered one when slugs is
// empty. One failure does not stop the rest.
func (m migrator) tenants(ctx context.Context, slugs []string) error {
	tenants, err := m.resolve(ctx, slugs)
	if err != nil {
		return err
	}
	failed := 0
	for i := range tenants {
		if err := m.apply(ctx, migrations.Tenant, tenantLabel(&tenants[i])); err != nil {
			fmt.Fprintln(os.Stderr, "migrate:", err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d tenants failed", failed, len(tenants))
	}
	return nil
}

// resolve takes slugs, which is what an operator knows a tenant by, and checks
// them against the registry, so a typo cannot create a schema nobody owns.
func (m migrator) resolve(ctx context.Context, slugs []string) ([]domain.Tenant, error) {
	if err := migrations.Check(ctx, m.db, migrations.Shared, migrations.SharedSchema); err != nil {
		return nil, err
	}
	if len(slugs) == 0 {
		return m.everyTenant(ctx)
	}
	out := make([]domain.Tenant, 0, len(slugs))
	for _, slug := range slugs {
		t, err := m.registry.BySlug(ctx, slug)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", slug, err)
		}
		out = append(out, t)
	}
	return out, nil
}

// everyTenant walks the registry a page at a time: a list is paged, and
// migrating all but the last few tenants would be worse than failing.
func (m migrator) everyTenant(ctx context.Context) ([]domain.Tenant, error) {
	var out []domain.Tenant
	page := domain.Page{Limit: domain.MaxLimit}
	for {
		batch, err := m.registry.Query(ctx, domain.TenantFilter{Page: page})
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
		if len(batch) < page.Limit {
			return out, nil
		}
		page.Offset += page.Limit
	}
}

type target struct {
	schema, label string
}

func tenantLabel(t *domain.Tenant) target {
	schema := domain.TenantSchema(t.ID)
	return target{schema: schema, label: schema + " (" + t.Slug + ")"}
}

func (m migrator) apply(ctx context.Context, c migrations.Category, t target) error {
	n, err := migrations.Apply(ctx, m.db, c, t.schema)
	if err != nil {
		return fmt.Errorf("%s: %w", t.label, err)
	}
	fmt.Printf("%-44s applied %d, at %d\n", t.label, n, migrations.Latest(c))
	return nil
}

func (m migrator) status(ctx context.Context) error {
	if err := m.printVersion(ctx, migrations.Shared, sharedTarget); err != nil {
		return err
	}
	tenants, err := m.resolve(ctx, nil)
	if err != nil {
		return err
	}
	for i := range tenants {
		if err := m.printVersion(ctx, migrations.Tenant, tenantLabel(&tenants[i])); err != nil {
			return err
		}
	}
	return nil
}

func (m migrator) printVersion(ctx context.Context, c migrations.Category, t target) error {
	have, err := migrations.Version(ctx, m.db, t.schema)
	if err != nil {
		return fmt.Errorf("%s: %w", t.label, err)
	}
	state := "ok"
	if have < migrations.Latest(c) {
		state = "behind"
	}
	fmt.Printf("%-44s %d of %d  %s\n", t.label, have, migrations.Latest(c), state)
	return nil
}
