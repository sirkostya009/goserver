package repository

import (
	"context"
	"time"

	"goserver/domain"
	"goserver/internal/auth"
	"goserver/internal/cache"
)

const (
	cacheTTL      = 5 * time.Minute
	cacheCapacity = 1 << 14
)

// table is a set of tables a transaction has written, which it then reads past
// the cache: the cache holds only committed rows.
type table uint8

const (
	tTenants table = 1 << iota
	tUsers
	tPosts
	tSessions
	tAPIKeys

	// a user takes their posts, sessions and keys with them
	tUsersCascade = tUsers | tPosts | tSessions | tAPIKeys
)

// rowKey is a row of a tenant table, by id.
type rowKey struct {
	tenant domain.TenantID
	id     domain.ID
}

// tokenKey is a session or a key, by the hash of its token.
type tokenKey struct {
	tenant domain.TenantID
	hash   auth.Hash
}

func rowKeyOf(ctx context.Context, id domain.ID) rowKey {
	return rowKey{domain.MustTenant(ctx).ID, id}
}

func tokenKeyOf(ctx context.Context, hash auth.Hash) tokenKey {
	return tokenKey{domain.MustTenant(ctx).ID, hash}
}

// newMap is a cache of one table. Every map shares the one clock, so a
// transaction's ticket holds for all of them.
func newMap[K comparable, V any](clock *cache.Clock) *cache.Map[K, V] {
	return cache.New[K, V](clock, cacheCapacity, cacheTTL)
}

// cached reads k from m, and on a miss runs read and fills m with its row. A
// transaction that wrote t reads past m and fills nothing.
func cached[K comparable, V any](ctx context.Context, clock *cache.Clock, m *cache.Map[K, V], t table, k K, read func() (V, error)) (V, error) {
	ticket := clock.Ticket()
	if st, ok := txStateFrom(ctx); ok {
		if st.dirty&t != 0 {
			return read()
		}
		ticket = st.ticket
	}
	if v, ok := m.Get(k); ok {
		return v, nil
	}
	v, err := read()
	if err == nil {
		m.Fill(k, v, ticket)
	}
	return v, err
}
