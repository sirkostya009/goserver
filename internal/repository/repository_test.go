package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"uuid"

	"github.com/sirkostya009/barm"

	"goserver/domain"
	"goserver/internal/migrations"
	"goserver/internal/pgtest"
)

type fixture struct {
	db       *barm.DB
	txer     Txer
	tenants  TenantRepository
	users    UserRepository
	posts    PostRepository
	sessions SessionRepository
	ids      map[string]domain.TenantID
}

// setup registers and migrates one tenant per slug.
func setup(t *testing.T, slugs ...string) fixture {
	t.Helper()
	return setupWith(t, nil, slugs...)
}

// setupWith is setup on a database opened with opts.
func setupWith(t *testing.T, opts []barm.Option, slugs ...string) fixture {
	t.Helper()
	ctx := context.Background()
	url := pgtest.NewDatabase(t)

	admin, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	migrate(t, admin, migrations.Shared, migrations.SharedSchema)

	db, err := OpenChecked(ctx, url, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	repos := New(db)
	f := fixture{db, repos.Txer, repos.Tenants, repos.Users, repos.Posts, repos.Sessions, map[string]domain.TenantID{}}
	for _, slug := range slugs {
		tenant, err := f.tenants.Insert(ctx, domain.CreateTenant{Slug: slug, Name: slug})
		if err != nil {
			t.Fatal(err)
		}
		f.ids[slug] = tenant.ID
		migrate(t, admin, migrations.Tenant, domain.TenantSchema(f.ids[slug]))
	}
	return f
}

// as is the context of a request acting for tenant.
func as(tenant domain.TenantID) context.Context {
	return domain.WithTenant(context.Background(), tenant)
}

// migrate stands in for cmd/migrate: the repositories never migrate.
func migrate(t *testing.T, db *barm.DB, c migrations.Category, schema string) {
	t.Helper()
	if _, err := migrations.Apply(context.Background(), db, c, schema); err != nil {
		t.Fatal(err)
	}
}

func TestTenants_SeparateSchemas(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setup(t, "acme", "globex")
	acme, globex := f.ids["acme"], f.ids["globex"]

	guy, err := f.users.Insert(as(acme), domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if guy.ID == (domain.ID{}) || uuid.UUID(guy.ID).String()[14] != '7' || guy.CreatedAt.IsZero() {
		t.Errorf("database did not hand back a v7 id and created_at: %+v", guy)
	}

	if _, err := f.users.Get(as(globex), guy.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("globex sees acme's user: %v", err)
	}
	if got, _ := f.users.Query(as(globex), domain.UserFilter{}); len(got) != 0 {
		t.Errorf("globex list: %v", got)
	}

	// the same email is free in another tenant, taken in the same one
	if _, err := f.users.Insert(as(globex), domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, ""); err != nil {
		t.Errorf("same email, other tenant: %v", err)
	}
	if _, err := f.users.Insert(as(acme), domain.CreateUser{Name: "Dup", Email: "guy@acme.io"}, ""); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("same email, same tenant: %v", err)
	}

	n, err := f.db.Select[int]().Table("information_schema.tables").ColumnExpr("count(*)").
		Where("table_name = 'users' AND table_schema IN (?, ?)", domain.TenantSchema(acme), domain.TenantSchema(globex)).
		One(ctx)
	if err != nil || n != 2 {
		t.Errorf("want a users table per tenant schema: %d %v", n, err)
	}
}

func TestTenant_Unknown(t *testing.T) {
	t.Parallel()
	f := setup(t, "acme")
	nobody := domain.TenantID(uuid.NewV7())
	if _, err := f.users.Query(as(nobody), domain.UserFilter{}); !errors.Is(err, domain.ErrUnknownTenant) {
		t.Fatalf("got %v", err)
	}
	if _, err := f.users.Insert(as(nobody), domain.CreateUser{Name: "x", Email: "x@x.io"}, ""); !errors.Is(err, domain.ErrUnknownTenant) {
		t.Fatalf("insert: %v", err)
	}
}

func TestPosts_Constraints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setup(t, "acme")
	acme := f.ids["acme"]

	if _, err := f.posts.Insert(as(acme), domain.CreatePost{AuthorID: domain.ID(uuid.NewV7()), Title: "x"}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("dangling author: %v", err)
	}

	guy, err := f.users.Insert(as(acme), domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.posts.Insert(as(acme), domain.CreatePost{AuthorID: guy.ID, Title: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := f.users.Delete(as(acme), guy.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.posts.Query(as(acme), domain.PostFilter{}); len(got) != 0 {
		t.Errorf("posts survived their author: %v", got)
	}
	if err := f.users.Delete(as(acme), guy.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}

	if _, err := f.tenants.Insert(ctx, domain.CreateTenant{Slug: "acme", Name: "again"}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("duplicate slug: %v", err)
	}
}

// barm hands back an empty slice for no rows: a nil one would reach the wire as null
func TestLists_EmptyNotNil(t *testing.T) {
	t.Parallel()
	f := setup(t, "acme")
	acme := f.ids["acme"]

	if users, err := f.users.Query(as(acme), domain.UserFilter{}); err != nil || users == nil {
		t.Errorf("users: %v %v", users, err)
	}
	if posts, err := f.posts.Query(as(acme), domain.PostFilter{}); err != nil || posts == nil {
		t.Errorf("posts: %v %v", posts, err)
	}
	if posts, err := f.posts.Query(as(acme), domain.PostFilter{AuthorIDs: []domain.ID{domain.ID(uuid.NewV7())}}); err != nil || posts == nil {
		t.Errorf("posts by author: %v %v", posts, err)
	}
}

func TestPosts_Queries(t *testing.T) {
	t.Parallel()
	f := setup(t, "acme")
	acme := f.ids["acme"]

	guy, err := f.users.Insert(as(acme), domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}
	hank, err := f.users.Insert(as(acme), domain.CreateUser{Name: "Hank", Email: "hank@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []domain.CreatePost{
		{AuthorID: guy.ID, Title: "first", Body: "a"},
		{AuthorID: hank.ID, Title: "second"},
		{AuthorID: guy.ID, Title: "third"},
	} {
		if _, err := f.posts.Insert(as(acme), p); err != nil {
			t.Fatal(err)
		}
	}

	all, err := f.posts.Query(as(acme), domain.PostFilter{})
	if err != nil || len(all) != 3 || all[0].Title != "third" || all[2].Title != "first" {
		t.Errorf("list is not newest first: %v %v", all, err)
	}
	mine, err := f.posts.Query(as(acme), domain.PostFilter{AuthorIDs: []domain.ID{guy.ID}})
	if err != nil || len(mine) != 2 || mine[0].Title != "third" || mine[1].Title != "first" {
		t.Errorf("by author: %v %v", mine, err)
	}

	got, err := f.posts.Get(as(acme), all[2].ID)
	if err != nil || got.Title != "first" || got.Body != "a" || got.AuthorID != guy.ID || got.CreatedAt.IsZero() {
		t.Errorf("get: %+v %v", got, err)
	}
	if _, err := f.posts.Get(as(acme), domain.ID(uuid.NewV7())); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get missing: %v", err)
	}

	if err := f.posts.Delete(as(acme), got.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.posts.Delete(as(acme), got.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}

	users, err := f.users.Query(as(acme), domain.UserFilter{})
	if err != nil || len(users) != 2 || users[0].Name != "Guy" || users[1].Name != "Hank" {
		t.Errorf("users are not in id order: %v %v", users, err)
	}
}

func TestTenants_Registry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setup(t, "globex", "acme")

	all, err := f.tenants.Query(ctx, domain.TenantFilter{})
	if err != nil || len(all) != 2 || all[0].Slug != "acme" || all[1].Slug != "globex" {
		t.Errorf("list is not in slug order: %v %v", all, err)
	}
	if all[0].Name != "acme" || all[0].ID == (domain.TenantID{}) || all[0].CreatedAt.IsZero() {
		t.Errorf("columns not scanned: %+v", all[0])
	}

	if got, err := f.tenants.BySlug(ctx, "acme"); err != nil || got.ID != f.ids["acme"] {
		t.Errorf("by slug: %+v %v", got, err)
	}
	if _, err := f.tenants.BySlug(ctx, "nobody"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("by unknown slug: %v", err)
	}
}

func TestBackend_DoesNotMigrate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	if _, err := OpenChecked(ctx, pgtest.NewDatabase(t)); err == nil {
		t.Error("unmigrated database opened")
	} else if _, ok := errors.AsType[*migrations.BehindError](err); !ok {
		t.Errorf("unmigrated database: %v", err)
	}

	f := setup(t)
	fresh, err := f.tenants.Insert(ctx, domain.CreateTenant{Slug: "fresh", Name: "Fresh"})
	if err != nil {
		t.Fatal(err)
	}
	id := fresh.ID
	if _, err := f.users.Query(as(id), domain.UserFilter{}); !errors.Is(err, domain.ErrUnknownTenant) {
		t.Fatalf("registered but unmigrated tenant: %v", err)
	}

	// no restart needed once the migration has run
	migrate(t, f.db, migrations.Tenant, domain.TenantSchema(id))
	if _, err := f.users.Query(as(id), domain.UserFilter{}); err != nil {
		t.Errorf("after migrating: %v", err)
	}
}

func TestIDs_NativeUUIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setup(t, "acme")
	acme := f.ids["acme"]

	guy, err := f.users.Insert(as(acme), domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}
	post, err := f.posts.Insert(as(acme), domain.CreatePost{AuthorID: guy.ID, Title: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if post.ID == (domain.ID{}) || post.AuthorID != guy.ID || post.CreatedAt.IsZero() {
		t.Errorf("insert did not hand back the stored row: %+v", post)
	}

	type stored struct {
		IDType     string `barm:"id_type"`
		AuthorType string `barm:"author_type"`
		Version    int    `barm:"version"`
	}
	got, err := f.db.Select[stored]().Table(domain.TenantSchema(acme) + ".posts").
		ColumnExpr("pg_typeof(id)::text AS id_type, pg_typeof(author_id)::text AS author_type, uuid_extract_version(id) AS version").
		One(ctx)
	if err != nil || got.IDType != "uuid" || got.AuthorType != "uuid" || got.Version != 7 {
		t.Errorf("stored as %s/%s v%d: %v", got.IDType, got.AuthorType, got.Version, err)
	}
}

// the api guard guarantees a tenant, so reaching a tenant repository without
// one is a wiring bug and must not quietly read some default schema
func TestTenant_MissingFromContext_Panics(t *testing.T) {
	t.Parallel()
	f := setup(t, "acme")
	defer func() {
		if recover() == nil {
			t.Error("no panic")
		}
	}()
	_, _ = f.users.Query(context.Background(), domain.UserFilter{})
}

func TestTx_TakenFromContext(t *testing.T) {
	t.Parallel()
	f := setup(t, "acme")
	outer := as(f.ids["acme"])

	// what a rolled back transaction wrote is not there afterwards
	ctx := outer
	tx, err := f.txer.Begin(&ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.users.Insert(ctx, domain.CreateUser{Name: "Gone", Email: "gone@acme.io"}, ""); err != nil {
		t.Fatal(err)
	}
	// the write is visible inside the transaction and nowhere else
	if in, _ := f.users.Query(ctx, domain.UserFilter{}); len(in) != 1 {
		t.Errorf("the transaction cannot see its own write: %v", in)
	}
	if out, _ := f.users.Query(outer, domain.UserFilter{}); len(out) != 0 {
		t.Errorf("an uncommitted write is visible outside: %v", out)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if out, _ := f.users.Query(outer, domain.UserFilter{}); len(out) != 0 {
		t.Errorf("rolled back write survived: %v", out)
	}

	// and what a committed one wrote is
	ctx = outer
	tx, err = f.txer.Begin(&ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.users.Insert(ctx, domain.CreateUser{Name: "Kept", Email: "kept@acme.io"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// the deferred rollback every caller writes undoes nothing after a commit,
	// it only says the transaction is over, which is why callers ignore it
	if err := tx.Rollback(); !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("rollback after commit: %v", err)
	}
	out, err := f.users.Query(outer, domain.UserFilter{})
	if err != nil || len(out) != 1 || out[0].Name != "Kept" {
		t.Errorf("committed write missing: %v %v", out, err)
	}
}

// a transaction begun inside one is a savepoint: rolling it back keeps the work
// around it
func TestTx_Nested_IsSavepoint(t *testing.T) {
	t.Parallel()
	f := setup(t, "acme")
	outer := as(f.ids["acme"])

	ctx := outer
	tx, err := f.txer.Begin(&ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := f.users.Insert(ctx, domain.CreateUser{Name: "Kept", Email: "kept@acme.io"}, ""); err != nil {
		t.Fatal(err)
	}

	inner := ctx
	nested, err := f.txer.Begin(&inner, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.users.Insert(inner, domain.CreateUser{Name: "Gone", Email: "gone@acme.io"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := nested.Rollback(); err != nil {
		t.Fatal(err)
	}

	got, err := f.users.Query(ctx, domain.UserFilter{})
	if err != nil || len(got) != 1 || got[0].Name != "Kept" {
		t.Errorf("the savepoint took its surroundings with it: %v %v", got, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.users.Query(outer, domain.UserFilter{}); len(got) != 1 {
		t.Errorf("after commit: %v", got)
	}
}
