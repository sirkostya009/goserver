package domain

import (
	"cmp"
	"strings"

	"github.com/sirkostya009/barm"
)

// A filter's Q is where it becomes SQL, and is passed to barm's Apply:
//
//	db.Select[Post]().Schema(s).Apply(f.Q).Slice(ctx)
//
// Nothing from a filter reaches the query text: values bind as arguments, and a
// sort column is looked up in a table of the ones that exist. Every Q pages.

func (f UserFilter) Q(q *barm.SelectQuery[User]) *barm.SelectQuery[User] {
	if len(f.IDs) > 0 {
		q = q.Where("id = ANY(?)", f.IDs)
	}
	if f.NameLike != "" {
		q = q.Where("name ILIKE ?", likeContains(f.NameLike))
	}
	if f.EmailLike != "" {
		q = q.Where("email ILIKE ?", likeContains(f.EmailLike))
	}
	if f.CreatedAfter != nil {
		q = q.Where("created_at >= ?", *f.CreatedAfter)
	}
	if f.CreatedBefore != nil {
		q = q.Where("created_at < ?", *f.CreatedBefore)
	}
	return paged(q, cmp.Or(userSortColumns[f.Sort], "id"), f.Desc, f.Page)
}

func (f PostFilter) Q(q *barm.SelectQuery[Post]) *barm.SelectQuery[Post] {
	if len(f.IDs) > 0 {
		q = q.Where("id = ANY(?)", f.IDs)
	}
	if len(f.AuthorIDs) > 0 {
		q = q.Where("author_id = ANY(?)", f.AuthorIDs)
	}
	if f.TitleLike != "" {
		q = q.Where("title ILIKE ?", likeContains(f.TitleLike))
	}
	if f.BodyLike != "" {
		q = q.Where("body ILIKE ?", likeContains(f.BodyLike))
	}
	if f.CreatedAfter != nil {
		q = q.Where("created_at >= ?", *f.CreatedAfter)
	}
	if f.CreatedBefore != nil {
		q = q.Where("created_at < ?", *f.CreatedBefore)
	}
	// newest first by default: a post list is read from the top
	return paged(q, cmp.Or(postSortColumns[f.Sort], "id"), !f.Desc, f.Page)
}

func (f TenantFilter) Q(q *barm.SelectQuery[Tenant]) *barm.SelectQuery[Tenant] {
	if len(f.IDs) > 0 {
		q = q.Where("id = ANY(?)", f.IDs)
	}
	if f.SlugLike != "" {
		q = q.Where("slug ILIKE ?", likeContains(f.SlugLike))
	}
	if f.NameLike != "" {
		q = q.Where("name ILIKE ?", likeContains(f.NameLike))
	}
	return paged(q, "slug", f.Desc, f.Page)
}

// paged orders by a column that was looked up, never the caller's text, and
// cuts out the page.
func paged[T any](q *barm.SelectQuery[T], column string, desc bool, p Page) *barm.SelectQuery[T] {
	if desc {
		column += " DESC"
	}
	p = p.OrDefault()
	return q.OrderBy(column).Limit(int64(p.Limit)).Offset(int64(p.Offset))
}

// likeContains turns a caller's text into a LIKE pattern matching it anywhere.
// Inside a pattern `%` and `_` are wildcards and `\` escapes them, so all three
// have to be escaped for the text to match itself.
func likeContains(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('%')
	for i := range len(s) {
		if c := s[i]; c == '%' || c == '_' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	b.WriteByte('%')
	return b.String()
}

// a sort that is not in its table, empty included, orders by id

var userSortColumns = map[UserSort]string{
	UserSortID:        "id",
	UserSortName:      "name",
	UserSortEmail:     "email",
	UserSortCreatedAt: "created_at",
}

var postSortColumns = map[PostSort]string{
	PostSortID:        "id",
	PostSortTitle:     "title",
	PostSortCreatedAt: "created_at",
}
