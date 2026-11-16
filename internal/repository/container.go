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
	posts := newTableCache[rowKey, domain.Post](clock, tPosts)
	sessions := newTableCache[tokenKey, Session](clock, tSessions)
	apiKeys := newTableCache[tokenKey, domain.APIKey](clock, tAPIKeys)
	return Container{
		Tenants:  &cachedTenantRepository{c.Tenants, newTableCache[string, domain.Tenant](clock, tTenants)},
		Users:    &cachedUserRepository{c.Users, newTableCache[rowKey, domain.User](clock, tUsers), posts, sessions, apiKeys},
		Posts:    &cachedPostRepository{c.Posts, posts},
		Sessions: &cachedSessionRepository{c.Sessions.(sessionStore), sessions}, //nolint:forcetypeassert // New builds the concrete repositories, which var _ checks
		APIKeys:  &cachedAPIKeyRepository{c.APIKeys.(apiKeyStore), apiKeys},     //nolint:forcetypeassert // likewise
		Txer:     cachedTxer{c.Txer, clock},
	}
}
