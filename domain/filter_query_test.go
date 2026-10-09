package domain

import (
	"reflect"
	"testing"
	"time"

	"github.com/sirkostya009/barm"
)

var builder = barm.NewBuilder(barm.Postgres)

const (
	postColumns   = `SELECT "p"."id", "p"."author_id", "p"."title", "p"."body", "p"."created_at" FROM "posts" AS "p"`
	userColumns   = `SELECT "u"."id", "u"."name", "u"."email", "u"."created_at" FROM "users" AS "u"`
	tenantColumns = `SELECT "t"."id", "t"."slug", "t"."name", "t"."created_at" FROM "shared"."tenants" AS "t"`
)

func wantSQL(t *testing.T, name, gotSQL string, gotArgs []any, err error, wantSQL string, wantArgs ...any) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if gotSQL != wantSQL {
		t.Errorf("%s:\n got %s\nwant %s", name, gotSQL, wantSQL)
	}
	if len(gotArgs) != len(wantArgs) || (len(wantArgs) > 0 && !reflect.DeepEqual(gotArgs, wantArgs)) {
		t.Errorf("%s: args %v, want %v", name, gotArgs, wantArgs)
	}
}

func TestPostFilter_Q(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a, b := ID{1}, ID{2}

	s, args, err := builder.Select[Post]().Apply(PostFilter{}.Q).Build()
	// newest first by default
	wantSQL(t, "empty", s, args, err, postColumns+` ORDER BY id DESC LIMIT 50`)

	s, args, err = builder.Select[Post]().Apply(PostFilter{
		AuthorIDs: []ID{a, b}, TitleLike: `50%_off\`, CreatedAfter: &at, CreatedBefore: &at,
		Sort: PostSortTitle, Desc: true, Page: Page{Limit: 5, Offset: 10},
	}.Q).Build()
	wantSQL(t, "full", s, args, err,
		postColumns+` WHERE (author_id = ANY($1)) AND (title ILIKE $2) AND (created_at >= $3) AND (created_at < $4) ORDER BY title LIMIT 5 OFFSET 10`,
		[]ID{a, b}, `%50\%\_off\\%`, at, at)
}

func TestUserFilter_Q(t *testing.T) {
	t.Parallel()
	s, args, err := builder.Select[User]().Apply(UserFilter{}.Q).Build()
	wantSQL(t, "empty", s, args, err, userColumns+` ORDER BY id LIMIT 50`)

	s, args, err = builder.Select[User]().Apply(UserFilter{NameLike: "hill", EmailLike: "@acme.io", Sort: UserSortEmail, Desc: true}.Q).Build()
	wantSQL(t, "full", s, args, err,
		userColumns+` WHERE (name ILIKE $1) AND (email ILIKE $2) ORDER BY email DESC LIMIT 50`, "%hill%", "%@acme.io%")
}

func TestTenantFilter_Q(t *testing.T) {
	t.Parallel()
	id := TenantID{1}
	s, args, err := builder.Select[Tenant]().Apply(TenantFilter{IDs: []TenantID{id}, SlugLike: "ac", Desc: true, Page: Page{Offset: 3}}.Q).Build()
	wantSQL(t, "full", s, args, err,
		tenantColumns+` WHERE (id = ANY($1)) AND (slug ILIKE $2) ORDER BY slug DESC LIMIT 50 OFFSET 3`, []TenantID{id}, "%ac%")
}

// decoding rejects an unknown sort, but Q may be called from anywhere: whatever
// the value, only a column from the table reaches the text
func TestFilter_Q_UnknownSort_OrdersByID(t *testing.T) {
	t.Parallel()
	s, args, err := builder.Select[User]().Apply(UserFilter{Sort: "name; DROP TABLE users"}.Q).Build()
	wantSQL(t, "user", s, args, err, userColumns+` ORDER BY id LIMIT 50`)
	s, args, err = builder.Select[Post]().Apply(PostFilter{Sort: "1"}.Q).Build()
	wantSQL(t, "post", s, args, err, postColumns+` ORDER BY id DESC LIMIT 50`)
}
