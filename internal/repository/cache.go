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

// tableCache is the cache of one table. Every one shares the clock, so a
// transaction's ticket holds for all of them.
type tableCache[K comparable, V any] struct {
	m     *cache.Map[K, V]
	clock *cache.Clock
	table table
}

func newTableCache[K comparable, V any](clock *cache.Clock, t table) *tableCache[K, V] {
	return &tableCache[K, V]{cache.New[K, V](clock, cacheCapacity, cacheTTL), clock, t}
}

// get reads k, and on a miss runs read and fills k with its row. A transaction
// that wrote the table reads past the cache and fills nothing.
func (c *tableCache[K, V]) get(ctx context.Context, k K, read func() (V, error)) (V, error) {
	ticket := c.clock.Ticket()
	if st, ok := txStateFrom(ctx); ok {
		if st.dirty&c.table != 0 {
			return read()
		}
		ticket = st.ticket
	}
	if v, ok := c.m.Get(k); ok {
		return v, nil
	}
	v, err := read()
	if err == nil {
		c.m.Fill(k, v, ticket)
	}
	return v, err
}

// wrote marks the table written by ctx's transaction.
func (c *tableCache[K, V]) wrote(ctx context.Context) { afterWrite(ctx, c.table, nil) }

// drop is wrote, and invalidates k once the write is durable.
func (c *tableCache[K, V]) drop(ctx context.Context, k K) {
	afterWrite(ctx, c.table, func() { c.m.Invalidate(k) })
}

// dropFunc is drop of every entry f reports. It visits the whole cache, so it
// is for writes too rare to index for.
func (c *tableCache[K, V]) dropFunc(ctx context.Context, f func(K, V) bool) {
	afterWrite(ctx, c.table, func() { c.m.InvalidateFunc(f) })
}
