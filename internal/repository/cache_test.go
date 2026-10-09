package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"goserver/domain"
	"goserver/internal/auth"
)

// setupCached is setup on the cached repositories.
func setupCached(t *testing.T, slugs ...string) fixture {
	t.Helper()
	f := setup(t, slugs...)
	repos := NewCached(f.db)
	f.txer, f.tenants, f.users, f.posts, f.sessions = repos.Txer, repos.Tenants, repos.Users, repos.Posts, repos.Sessions
	return f
}

// rename names a user "Renamed" behind the repositories' back, so a read that
// still sees the old name came from the cache.
func rename(t *testing.T, f fixture, tenant domain.TenantID, id domain.ID) {
	t.Helper()
	q := `UPDATE "` + domain.TenantSchema(tenant) + `".users SET name = $1 WHERE id = $2`
	if _, err := f.db.Exec(context.Background(), q, "Renamed", id); err != nil {
		t.Fatal(err)
	}
}

func getName(t *testing.T, f fixture, ctx context.Context, id domain.ID) string {
	t.Helper()
	u, err := f.users.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return u.Name
}

func TestNew_DoesNotCache(t *testing.T) {
	t.Parallel()
	f := setup(t, "acme")
	ctx := as(f.ids["acme"])

	guy, err := f.users.Insert(ctx, domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}
	getName(t, f, ctx, guy.ID)
	rename(t, f, f.ids["acme"], guy.ID)
	if got := getName(t, f, ctx, guy.ID); got != "Renamed" {
		t.Errorf("cached: %q", got)
	}
}

func TestCache_Get_ServedFromCache(t *testing.T) {
	t.Parallel()
	f := setupCached(t, "acme")
	ctx := as(f.ids["acme"])

	guy, err := f.users.Insert(ctx, domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}
	getName(t, f, ctx, guy.ID)
	rename(t, f, f.ids["acme"], guy.ID)
	if got := getName(t, f, ctx, guy.ID); got != "Guy" {
		t.Errorf("not cached: %q", got)
	}

	if err := f.users.Delete(ctx, guy.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.users.Get(ctx, guy.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleted user still served: %v", err)
	}
}

func TestCache_Get_KeyedByTenant(t *testing.T) {
	t.Parallel()
	f := setupCached(t, "acme", "globex")

	guy, err := f.users.Insert(as(f.ids["acme"]), domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}
	getName(t, f, as(f.ids["acme"]), guy.ID)
	if _, err := f.users.Get(as(f.ids["globex"]), guy.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("another tenant's cached row: %v", err)
	}
}

// a transaction that has written nothing reads committed rows, which the cache
// holds; once it writes a table it reads that table from the database
func TestCache_Tx_DirtyReadsDatabase(t *testing.T) {
	t.Parallel()
	f := setupCached(t, "acme")
	outer := as(f.ids["acme"])

	guy, err := f.users.Insert(outer, domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}
	getName(t, f, outer, guy.ID)
	rename(t, f, f.ids["acme"], guy.ID)

	ctx := outer
	tx, err := f.txer.Begin(&ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if got := getName(t, f, ctx, guy.ID); got != "Guy" {
		t.Errorf("clean tx missed the cache: %q", got)
	}
	if _, err := f.posts.Insert(ctx, domain.CreatePost{AuthorID: guy.ID, Title: "x"}); err != nil {
		t.Fatal(err)
	}
	if got := getName(t, f, ctx, guy.ID); got != "Guy" {
		t.Errorf("a write to posts dirtied users: %q", got)
	}
	if _, err := f.users.Insert(ctx, domain.CreateUser{Name: "Other", Email: "other@acme.io"}, ""); err != nil {
		t.Fatal(err)
	}
	if got := getName(t, f, ctx, guy.ID); got != "Renamed" {
		t.Errorf("dirty tx read the cache: %q", got)
	}
}

// what a transaction reads after writing is uncommitted, and must not outlive
// its rollback in the cache
func TestCache_Tx_DirtyDoesNotFill(t *testing.T) {
	t.Parallel()
	f := setupCached(t, "acme")
	outer := as(f.ids["acme"])

	ctx := outer
	tx, err := f.txer.Begin(&ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	ghost, err := f.users.Insert(ctx, domain.CreateUser{Name: "Ghost", Email: "ghost@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}
	getName(t, f, ctx, ghost.ID)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.users.Get(outer, ghost.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("rolled back row served: %v", err)
	}
}

// invalidation waits for the commit: until then everyone else still sees the
// row, and a rollback keeps it
func TestCache_Tx_InvalidatesOnCommit(t *testing.T) {
	t.Parallel()
	f := setupCached(t, "acme")
	outer := as(f.ids["acme"])

	guy, err := f.users.Insert(outer, domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}
	getName(t, f, outer, guy.ID)

	ctx := outer
	tx, err := f.txer.Begin(&ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.users.Delete(ctx, guy.ID); err != nil {
		t.Fatal(err)
	}
	if got := getName(t, f, outer, guy.ID); got != "Guy" {
		t.Errorf("uncommitted delete invalidated: %q", got)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := getName(t, f, outer, guy.ID); got != "Guy" {
		t.Errorf("after rollback: %q", got)
	}

	ctx = outer
	tx, err = f.txer.Begin(&ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.users.Delete(ctx, guy.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.users.Get(outer, guy.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("committed delete still served: %v", err)
	}
}

// a savepoint rolled back takes its invalidations with it, and the outer
// transaction stays dirty: what the savepoint wrote is gone, but nothing says
// what else it touched
func TestCache_Tx_NestedRollback_DropsInvalidation(t *testing.T) {
	t.Parallel()
	f := setupCached(t, "acme")
	outer := as(f.ids["acme"])

	guy, err := f.users.Insert(outer, domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}
	getName(t, f, outer, guy.ID)
	rename(t, f, f.ids["acme"], guy.ID)

	ctx := outer
	tx, err := f.txer.Begin(&ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	inner := ctx
	nested, err := f.txer.Begin(&inner, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.users.Delete(inner, guy.ID); err != nil {
		t.Fatal(err)
	}
	if err := nested.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := getName(t, f, ctx, guy.ID); got != "Renamed" {
		t.Errorf("outer tx not dirty after savepoint write: %q", got)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := getName(t, f, outer, guy.ID); got != "Guy" {
		t.Errorf("rolled back savepoint invalidated: %q", got)
	}
}

// a user takes their sessions with them, so a cached session must go too
func TestCache_UserDelete_InvalidatesSessions(t *testing.T) {
	t.Parallel()
	f := setupCached(t, "acme")
	ctx := as(f.ids["acme"])

	guy, err := f.users.Insert(ctx, domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}
	_, hash := auth.NewToken(auth.Session, f.ids["acme"])
	if _, err := f.sessions.Insert(ctx, guy.ID, hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sessions.ByHash(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if err := f.users.Delete(ctx, guy.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sessions.ByHash(ctx, hash); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("session outlived its user: %v", err)
	}
}

func TestCache_SessionDelete_Invalidates(t *testing.T) {
	t.Parallel()
	f := setupCached(t, "acme")
	ctx := as(f.ids["acme"])

	guy, err := f.users.Insert(ctx, domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}
	_, hash := auth.NewToken(auth.Session, f.ids["acme"])
	s, err := f.sessions.Insert(ctx, guy.ID, hash, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.sessions.ByHash(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if err := f.sessions.Delete(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sessions.ByHash(ctx, hash); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("logged out session still opens: %v", err)
	}
	if err := f.sessions.Delete(ctx, s.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}
