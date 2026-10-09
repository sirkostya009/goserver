package client_test

import (
	"context"
	"errors"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"uuid"

	client "goserver/client/go"
	"goserver/domain"
	"goserver/internal/httpapi"
	"goserver/internal/migrations"
	"goserver/internal/pgtest"
	"goserver/internal/repository"
	"goserver/internal/service"
)

func TestMain(m *testing.M) {
	pgtest.Run(m)
}

// TestClient_E2E drives the generated client against the generated router and
// the real services, over TLS, since the session cookie is __Host-.
func TestClient_E2E(t *testing.T) {
	t.Parallel()
	db := pgtest.DB(t)
	srv := httptest.NewTLSServer(httpapi.New(db, service.New(repository.New(db)), "root"))
	t.Cleanup(srv.Close)
	ctx := t.Context()

	admin := client.New(srv.URL, client.Options{HTTPClient: srv.Client(), Token: "root"})
	tenant, err := admin.Tenants.Create(ctx, domain.CreateTenant{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrations.Apply(ctx, db, migrations.Tenant, domain.TenantSchema(tenant.ID)); err != nil {
		t.Fatal(err)
	}
	guy, err := admin.WithTenant(tenant.ID).Users.Create(ctx, domain.CreateUser{
		Name: "Guy", Email: "guy@acme.io", Password: "hunter22",
	})
	if err != nil {
		t.Fatal(err)
	}

	jar, _ := cookiejar.New(nil)
	browser := srv.Client()
	browser.Jar = jar
	session := client.New(srv.URL, client.Options{HTTPClient: browser})
	if _, err := session.Auth.Me(ctx); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("me before login: %v", err)
	}
	if u, err := session.Auth.Login(ctx, domain.Login{Tenant: "acme", Email: "guy@acme.io", Password: "hunter22"}); err != nil || u != guy {
		t.Fatalf("login: %+v, %v", u, err)
	}
	if u, err := session.Auth.Me(ctx); err != nil || u != guy {
		t.Fatalf("me: %+v, %v", u, err)
	}
	key, err := session.Keys.Create(ctx, domain.CreateAPIKey{Name: "ci"})
	if err != nil {
		t.Fatal(err)
	}

	bot := client.New(srv.URL, client.Options{HTTPClient: srv.Client(), Token: key.Token})
	if _, err := bot.Keys.Create(ctx, domain.CreateAPIKey{Name: "nope"}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("key made a key: %v", err)
	}
	post, err := bot.Posts.Create(ctx, domain.CreatePost{AuthorID: guy.ID, Title: "Hello", Body: "World"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := bot.Posts.Get(ctx, post.ID); err != nil || got != post {
		t.Errorf("get: %+v, %v", got, err)
	}
	if got, err := bot.Posts.Query(ctx, domain.PostFilter{TitleLike: "ell"}); err != nil || len(got) != 1 || got[0] != post {
		t.Errorf("query: %+v, %v", got, err)
	}
	if got, err := bot.Posts.ListByAuthor(ctx, guy.ID); err != nil || len(got) != 1 {
		t.Errorf("by author: %+v, %v", got, err)
	}
	if err := bot.Posts.Delete(ctx, post.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := bot.Posts.Get(ctx, post.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get after delete: %v", err)
	}

	ghost := domain.ID(uuid.NewV7())
	for name, run := range map[string]func(context.Context, []domain.CreatePost) (domain.PostImport, error){
		"batched": bot.Posts.Import, "unbatched": bot.Posts.ImportUnbatched,
	} {
		got, err := run(ctx, []domain.CreatePost{
			{AuthorID: guy.ID, Title: "one"},
			{AuthorID: ghost, Title: "ghost"},
			{AuthorID: guy.ID, Title: "two"},
		})
		if err != nil {
			t.Fatalf("%s import: %v", name, err)
		}
		if len(got.Posts) != 2 || got.Posts[1].Title != "two" || len(got.Failed) != 1 || got.Failed[0].Index != 1 {
			t.Errorf("%s import: %+v", name, got)
		}
		for _, p := range got.Posts {
			if err := bot.Posts.Delete(ctx, p.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	// nil is an empty array on the wire, not null
	if got, err := bot.Posts.Import(ctx, nil); err != nil || len(got.Posts)+len(got.Failed) != 0 {
		t.Errorf("empty import: %+v %v", got, err)
	}

	if err := session.Keys.Revoke(ctx, key.Key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := bot.Users.List(ctx); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("revoked key: %v", err)
	}
	if err := session.Auth.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Auth.Me(ctx); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("me after logout: %v", err)
	}
}
