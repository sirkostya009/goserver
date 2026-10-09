package service_test

import (
	"context"
	"testing"

	"github.com/sirkostya009/barm"

	"goserver/domain"
	"goserver/internal/migrations"
	"goserver/internal/pgtest"
	"goserver/internal/repository"
	"goserver/internal/service"
)

// The services run against a real postgres: every test gets a transaction on
// one shared database, rolled back when it ends, so what one test writes no
// other one sees.
//
// One consequence of that transaction: postgres refuses every further statement
// on a transaction after it has rejected one, so a test that expects the
// database to reject something has to end there. Split the rest into its own
// test. Outside tests those statements run on the pool, where a rejected one
// takes nothing with it.
func TestMain(m *testing.M) {
	pgtest.Run(m)
}

type fixture struct {
	DB       *barm.DB
	Repos    repository.Container
	Services service.Container
	t        *testing.T
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := pgtest.DB(t)
	repos := repository.New(db)
	return fixture{db, repos, service.New(repos), t}
}

// tenant registers a tenant, creates its schema, and returns the context a
// request for it would carry.
func (f fixture) tenant(slug string) context.Context {
	f.t.Helper()
	ctx := f.t.Context()
	tenant, err := f.Services.Tenants.Create(ctx, domain.CreateTenant{Slug: slug, Name: slug})
	if err != nil {
		f.t.Fatal(err)
	}
	// what cmd/migrate does for a tenant the api just registered
	if _, err := migrations.Apply(ctx, f.DB, migrations.Tenant, domain.TenantSchema(tenant.ID)); err != nil {
		f.t.Fatal(err)
	}
	return domain.WithTenant(ctx, tenant.ID)
}

// user is a user of that tenant, through the service that creates one.
func (f fixture) user(ctx context.Context, name, email string) domain.User {
	f.t.Helper()
	u, err := f.Services.Users.Create(ctx, domain.CreateUser{Name: name, Email: email, Password: "hunter22"})
	if err != nil {
		f.t.Fatal(err)
	}
	return u
}
