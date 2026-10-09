package domain

import (
	"errors"
	"time"
)

// A filter is the body of a QUERY request: anything with more than a couple of
// fields is clearer as json than as query parameters, and QUERY is the method
// that reads a body without pretending to change anything.

// Page is how much of a list to return. A zero limit means DefaultLimit, so a
// caller that asks for nothing still cannot pull a whole table.
//
//ggen:generate
type Page struct {
	Limit  int `json:"limit,omitempty" pipe:"gte=0 lte=200"`
	Offset int `json:"offset,omitempty" pipe:"gte=0"`
}

const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// OrDefault fills in the limit a caller left out.
func (p Page) OrDefault() Page {
	if p.Limit <= 0 {
		p.Limit = DefaultLimit
	}
	return p
}

// UserSort is the column a user list is ordered by. Empty means UserSortID.
type UserSort string

const (
	UserSortID        UserSort = "id"
	UserSortName      UserSort = "name"
	UserSortEmail     UserSort = "email"
	UserSortCreatedAt UserSort = "created_at"
)

var ErrBadSort = errors.New("unknown sort column")

func validUserSort(s UserSort) error {
	switch s {
	case "", UserSortID, UserSortName, UserSortEmail, UserSortCreatedAt:
		return nil
	}
	return ErrBadSort
}

// UserFilter narrows a user list. Every field is optional, and the ones that are
// set all have to match.
//
//ggen:generate
type UserFilter struct {
	IDs           []ID       `json:"ids,omitempty" pipe:"maxlen=100"`
	NameLike      string     `json:"name_like,omitempty" pipe:"trim maxrunes=64"`
	EmailLike     string     `json:"email_like,omitempty" pipe:"trim tolower maxlen=254"`
	CreatedAfter  *time.Time `json:"created_after,omitempty"`
	CreatedBefore *time.Time `json:"created_before,omitempty"`
	Sort          UserSort   `json:"sort,omitempty" pipe:"@validUserSort"`
	Desc          bool       `json:"desc,omitempty"`
	Page          Page       `json:"page,omitzero"`
}

// PostSort is the column a post list is ordered by. Empty means PostSortID,
// which is creation order, since ids are uuid v7.
type PostSort string

const (
	PostSortID        PostSort = "id"
	PostSortTitle     PostSort = "title"
	PostSortCreatedAt PostSort = "created_at"
)

func validPostSort(s PostSort) error {
	switch s {
	case "", PostSortID, PostSortTitle, PostSortCreatedAt:
		return nil
	}
	return ErrBadSort
}

// PostFilter narrows a post list.
//
//ggen:generate
type PostFilter struct {
	IDs           []ID       `json:"ids,omitempty" pipe:"maxlen=100"`
	AuthorIDs     []ID       `json:"author_ids,omitempty" pipe:"maxlen=100"`
	TitleLike     string     `json:"title_like,omitempty" pipe:"trim maxrunes=200"`
	BodyLike      string     `json:"body_like,omitempty" pipe:"trim maxrunes=200"`
	CreatedAfter  *time.Time `json:"created_after,omitempty"`
	CreatedBefore *time.Time `json:"created_before,omitempty"`
	Sort          PostSort   `json:"sort,omitempty" pipe:"@validPostSort"`
	Desc          bool       `json:"desc,omitempty"`
	Page          Page       `json:"page,omitzero"`
}

// TenantFilter narrows the registry. It is the one list nothing scopes, so it
// pages like the others.
//
//ggen:generate
type TenantFilter struct {
	IDs      []TenantID `json:"ids,omitempty" pipe:"maxlen=100"`
	SlugLike string     `json:"slug_like,omitempty" pipe:"trim tolower maxlen=32"`
	NameLike string     `json:"name_like,omitempty" pipe:"trim maxrunes=64"`
	Desc     bool       `json:"desc,omitempty"`
	Page     Page       `json:"page,omitzero"`
}
