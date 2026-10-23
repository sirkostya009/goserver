package service_test

import (
	"fmt"
	"strings"
	"testing"

	"goserver/domain"
	"goserver/internal/migrations"
	"goserver/internal/pgtest"
	"goserver/internal/repository"
	"goserver/internal/service"
)

func BenchmarkPosts_Import(b *testing.B) {
	ctx := b.Context()
	db, err := repository.Open(ctx, pgtest.NewDatabase(b))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { db.Close() })
	if _, err := migrations.Apply(ctx, db, migrations.Shared, migrations.SharedSchema); err != nil {
		b.Fatal(err)
	}
	svc := service.New(repository.New(db))
	tenant, err := svc.Tenants.Create(ctx, domain.CreateTenant{Slug: "acme", Name: "Acme"})
	if err != nil {
		b.Fatal(err)
	}
	if _, err := migrations.Apply(ctx, db, migrations.Tenant, domain.TenantSchema(tenant.ID)); err != nil {
		b.Fatal(err)
	}
	ctx = domain.WithTenant(ctx, tenant.ID)
	guy, err := svc.Users.Create(ctx, domain.CreateUser{Name: "Guy", Email: "guy@acme.io", Password: "hunter22"})
	if err != nil {
		b.Fatal(err)
	}
	post := `{"author_id":"` + guy.ID.String() + `","title":"a title","body":"some body text"}`

	for _, n := range []int{10, 100, 1000, 10_000} {
		json := "[" + strings.Repeat(post+",", n-1) + post + "]"
		for _, mode := range []string{"batched", "unbatched"} {
			run := svc.Posts.Import
			if mode == "unbatched" {
				run = svc.Posts.ImportUnbatched
			}
			b.Run(fmt.Sprintf("%s/%d", mode, n), func(b *testing.B) {
				for b.Loop() {
					if got, err := run(ctx, body(json)); err != nil || len(got.Posts) != n {
						b.Fatal(len(got.Posts), err)
					}
				}
			})
		}
	}
}
