package repository

import (
	"github.com/sirkostya009/barm"

	"goserver/domain"
	"goserver/internal/cache"
)

// Container is every repository there is, built on one database. A caller
// takes what it needs from it rather than constructing its own.
type Container struct {
	Tenants  TenantRepository
	Users    UserRepository
	Posts    PostRepository
	Sessions SessionRepository
	APIKeys  APIKeyRepository
	// Txer opens the transactions a service runs its repositories in.
	Txer Txer
}

// New wires the data layer. It does no I/O, and the caller owns db.
func New(db *barm.DB) Container {
	b := base{db}
	return Container{
		Tenants:  &tenantRepository{b},
		Users:    &userRepository{b},
		Posts:    &postRepository{b},
		Sessions: &sessionRepository{b},
		APIKeys:  &apiKeyRepository{b},
		Txer:     txer(b),
	}
}

// NewCached is New with point reads served from an in-process cache. The
// repositories share it, so build one Container per database.
func NewCached(db *barm.DB) Container {
	c := New(db)
	clock := &cache.Clock{}
	posts := newMap[rowKey, domain.Post](clock)
	sessions := newMap[tokenKey, Session](clock)
	apiKeys := newMap[tokenKey, domain.APIKey](clock)
	return Container{
		Tenants:  &cachedTenantRepository{c.Tenants, clock, newMap[string, domain.Tenant](clock)},
		Users:    &cachedUserRepository{c.Users, clock, newMap[rowKey, domain.User](clock), posts, sessions, apiKeys},
		Posts:    &cachedPostRepository{c.Posts, clock, posts},
		Sessions: &cachedSessionRepository{c.Sessions.(sessionStore), clock, sessions}, //nolint:forcetypeassert // New builds the concrete repositories, which var _ checks
		APIKeys:  &cachedAPIKeyRepository{c.APIKeys.(apiKeyStore), clock, apiKeys},     //nolint:forcetypeassert // likewise
		Txer:     cachedTxer{c.Txer, clock},
	}
}
