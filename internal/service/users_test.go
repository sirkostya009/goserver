package service_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
	"uuid"

	"goserver/domain"
)

func TestUsers_Create(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenant("acme")

	u, err := f.Services.Users.Create(ctx, domain.CreateUser{Name: "Guy", Email: "guy@acme.io", Password: "hunter22"})
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "Guy" || u.Email != "guy@acme.io" {
		t.Errorf("input not carried over: %+v", u)
	}
	// the row comes back as the database stored it
	if u.ID == (domain.ID{}) || uuid.UUID(u.ID).String()[14] != '7' || u.CreatedAt.IsZero() {
		t.Errorf("no generated id or timestamp: %+v", u)
	}

	if _, err := f.Services.Users.Create(ctx, domain.CreateUser{Name: "Dup", Email: "guy@acme.io", Password: "hunter22"}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("duplicate email: %v", err)
	}
}

func TestUsers_TenantScoped(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme, globex := f.tenant("acme"), f.tenant("globex")
	guy := f.user(acme, "Guy", "guy@acme.io")

	// the same address belongs to somebody else in another tenant
	if _, err := f.Services.Users.Create(globex, domain.CreateUser{Name: "Hank", Email: "guy@acme.io", Password: "hunter22"}); err != nil {
		t.Errorf("same email, other tenant: %v", err)
	}

	if got, err := f.Services.Users.Get(acme, guy.ID); err != nil || got != guy {
		t.Errorf("get: %+v %v", got, err)
	}
	if _, err := f.Services.Users.Get(globex, guy.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("another tenant can read the user: %v", err)
	}
	if err := f.Services.Users.Delete(globex, guy.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("another tenant can delete the user: %v", err)
	}

	mine, err := f.Services.Users.List(acme)
	if err != nil || len(mine) != 1 || mine[0] != guy {
		t.Errorf("list: %v %v", mine, err)
	}
	theirs, err := f.Services.Users.List(globex)
	if err != nil || len(theirs) != 1 || theirs[0].Name != "Hank" {
		t.Errorf("list of the other tenant: %v %v", theirs, err)
	}
}

func TestUsers_GetAndDelete_Unknown(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenant("acme")
	guy := f.user(ctx, "Guy", "guy@acme.io")

	if _, err := f.Services.Users.Get(ctx, domain.ID(uuid.NewV7())); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get unknown: %v", err)
	}
	if err := f.Services.Users.Delete(ctx, guy.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.Services.Users.Delete(ctx, guy.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
	if left, _ := f.Services.Users.List(ctx); len(left) != 0 {
		t.Errorf("deleted user is still listed: %v", left)
	}
}

// a tenant nobody registered has no schema, and that is what the api turns into
// a 404 rather than a 500
func TestUsers_UnknownTenant_Read(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := domain.WithTenant(context.Background(), domain.TenantID(uuid.NewV7()))

	if _, err := f.Services.Users.List(ctx); !errors.Is(err, domain.ErrUnknownTenant) {
		t.Errorf("list: %v", err)
	}
}

func TestUsers_UnknownTenant_Write(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := domain.WithTenant(context.Background(), domain.TenantID(uuid.NewV7()))

	if _, err := f.Services.Users.Create(ctx, domain.CreateUser{Name: "x", Email: "x@x.io", Password: "hunter22"}); !errors.Is(err, domain.ErrUnknownTenant) {
		t.Errorf("create: %v", err)
	}
}

func TestUsers_Query_Filters(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenant("acme")
	guy := f.user(ctx, "Guy Fawkes", "guy@acme.io")
	f.user(ctx, "Hank Hill", "hank@globex.com")
	ann := f.user(ctx, "Ann Perkins", "ann@acme.io")

	// the range is anchored to a stored timestamp rather than the wall clock:
	// created_at comes from the database, and inside a transaction its now() is
	// the moment the transaction began, which every row here shares
	stamped := guy.CreatedAt
	later := stamped.Add(time.Second)

	names := func(us []domain.User) []string {
		out := make([]string, len(us))
		for i, u := range us {
			out[i] = u.Name
		}
		return out
	}

	for name, tc := range map[string]struct {
		filter domain.UserFilter
		want   []string
	}{
		"nothing set":         {domain.UserFilter{}, []string{"Guy Fawkes", "Hank Hill", "Ann Perkins"}},
		"by id":               {domain.UserFilter{IDs: []domain.ID{ann.ID, guy.ID}}, []string{"Guy Fawkes", "Ann Perkins"}},
		"name substring":      {domain.UserFilter{NameLike: "Hill"}, []string{"Hank Hill"}},
		"name is caseless":    {domain.UserFilter{NameLike: "hILl"}, []string{"Hank Hill"}},
		"email domain":        {domain.UserFilter{EmailLike: "@acme.io"}, []string{"Guy Fawkes", "Ann Perkins"}},
		"two of them":         {domain.UserFilter{NameLike: "n", EmailLike: "@acme.io"}, []string{"Ann Perkins"}},
		"no match":            {domain.UserFilter{NameLike: "nobody"}, nil},
		"created at or after": {domain.UserFilter{CreatedAfter: &stamped}, []string{"Guy Fawkes", "Hank Hill", "Ann Perkins"}},
		"created before":      {domain.UserFilter{CreatedBefore: &stamped}, nil},
		"created within":      {domain.UserFilter{CreatedAfter: &stamped, CreatedBefore: &later}, []string{"Guy Fawkes", "Hank Hill", "Ann Perkins"}},
		"created after that":  {domain.UserFilter{CreatedAfter: &later}, nil},
		"sorted by name":      {domain.UserFilter{Sort: domain.UserSortName}, []string{"Ann Perkins", "Guy Fawkes", "Hank Hill"}},
		"sorted descending":   {domain.UserFilter{Sort: domain.UserSortName, Desc: true}, []string{"Hank Hill", "Guy Fawkes", "Ann Perkins"}},
		"first page":          {domain.UserFilter{Page: domain.Page{Limit: 2}}, []string{"Guy Fawkes", "Hank Hill"}},
		"second page":         {domain.UserFilter{Page: domain.Page{Limit: 2, Offset: 2}}, []string{"Ann Perkins"}},
		"past the end":        {domain.UserFilter{Page: domain.Page{Limit: 2, Offset: 99}}, nil},
		"page and order":      {domain.UserFilter{Sort: domain.UserSortEmail, Page: domain.Page{Limit: 1}}, []string{"Ann Perkins"}},
	} {
		got, err := f.Services.Users.Query(ctx, tc.filter)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(got) != len(tc.want) || (len(got) > 0 && !slices.Equal(names(got), tc.want)) {
			t.Errorf("%s: got %v, want %v", name, names(got), tc.want)
		}
	}
}

// a filter is text, and text with a wildcard in it must match that character
func TestUsers_Query_WildcardsAreLiteral(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenant("acme")
	f.user(ctx, "100%", "percent@acme.io")
	f.user(ctx, "100 of them", "hundred@acme.io")

	got, err := f.Services.Users.Query(ctx, domain.UserFilter{NameLike: "100%"})
	if err != nil || len(got) != 1 || got[0].Name != "100%" {
		t.Errorf("%% matched as a wildcard: %v %v", got, err)
	}
	if got, err := f.Services.Users.Query(ctx, domain.UserFilter{NameLike: "10_"}); err != nil || len(got) != 0 {
		t.Errorf("_ matched as a wildcard: %v %v", got, err)
	}
}

// the filter cannot reach another tenant's rows, whatever it asks for
func TestUsers_Query_TenantScoped(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme, globex := f.tenant("acme"), f.tenant("globex")
	guy := f.user(acme, "Guy", "guy@acme.io")

	got, err := f.Services.Users.Query(globex, domain.UserFilter{IDs: []domain.ID{guy.ID}})
	if err != nil || len(got) != 0 {
		t.Errorf("another tenant's user came back: %v %v", got, err)
	}
}

// an unfiltered list is the first page, not the table
func TestUsers_List_IsPaged(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenant("acme")
	for i := range domain.DefaultLimit + 5 {
		f.user(ctx, fmt.Sprintf("u%02d", i), fmt.Sprintf("u%02d@acme.io", i))
	}

	got, err := f.Services.Users.List(ctx)
	if err != nil || len(got) != domain.DefaultLimit {
		t.Errorf("got %d users, want the default limit %d: %v", len(got), domain.DefaultLimit, err)
	}
	rest, err := f.Services.Users.Query(ctx, domain.UserFilter{Page: domain.Page{Limit: domain.MaxLimit, Offset: domain.DefaultLimit}})
	if err != nil || len(rest) != 5 {
		t.Errorf("second page: %d %v", len(rest), err)
	}
}
