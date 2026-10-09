// Package cache is an in-process read-through cache that cannot be refilled
// with what an invalidation removed. A reader takes a ticket before it queries,
// and its fill is dropped when an invalidation landed in the same shard after
// the ticket: the row it read may be the one just invalidated.
package cache

import (
	"hash/maphash"
	"maps"
	"sync"
	"sync/atomic"
	"time"
)

const (
	shardCount = 64
	// evictSample is how many entries a full shard looks at to pick the one
	// closest to expiry.
	evictSample = 8
)

// Clock orders fills against invalidations. Maps that share one share tickets,
// so a transaction takes a single ticket for every map it reads.
type Clock struct{ n atomic.Uint64 }

// Ticket is taken before the read whose result will be filled.
func (c *Clock) Ticket() uint64 { return c.n.Load() }

func (c *Clock) tick() uint64 { return c.n.Add(1) }

type Map[K comparable, V any] struct {
	clock  *Clock
	seed   maphash.Seed
	ttl    int64
	limit  int
	shards [shardCount]shard[K, V]
}

type shard[K comparable, V any] struct {
	mu          sync.RWMutex
	m           map[K]entry[V]
	invalidated uint64
	_           [24]byte // pads to a cache line
}

type entry[V any] struct {
	v   V
	exp int64
}

// New makes a map holding about capacity entries, each for at most ttl.
func New[K comparable, V any](clock *Clock, capacity int, ttl time.Duration) *Map[K, V] {
	m := &Map[K, V]{clock: clock, seed: maphash.MakeSeed(), ttl: int64(ttl), limit: max(capacity/shardCount, 1)}
	for i := range m.shards {
		m.shards[i].m = make(map[K]entry[V])
	}
	return m
}

func (m *Map[K, V]) shard(k K) *shard[K, V] {
	return &m.shards[maphash.Comparable(m.seed, k)%shardCount]
}

func (m *Map[K, V]) Get(k K) (V, bool) {
	s := m.shard(k)
	s.mu.RLock()
	e, ok := s.m[k]
	s.mu.RUnlock()
	if !ok || e.exp <= time.Now().UnixNano() {
		var zero V
		return zero, false
	}
	return e.v, true
}

// Fill stores v read under ticket, unless an invalidation came after it.
func (m *Map[K, V]) Fill(k K, v V, ticket uint64) {
	now := time.Now().UnixNano()
	s := m.shard(k)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.invalidated > ticket {
		return
	}
	if _, ok := s.m[k]; !ok && len(s.m) >= m.limit {
		s.evict(now)
	}
	s.m[k] = entry[V]{v, now + m.ttl}
}

// evict drops an expired entry, or the sampled one closest to expiry.
func (s *shard[K, V]) evict(now int64) {
	var (
		victim K
		exp    int64
		n      int
	)
	for k, e := range s.m {
		if e.exp <= now {
			victim = k
			break
		}
		if n == 0 || e.exp < exp {
			victim, exp = k, e.exp
		}
		if n++; n == evictSample {
			break
		}
	}
	delete(s.m, victim)
}

func (m *Map[K, V]) Invalidate(k K) {
	t := m.clock.tick()
	s := m.shard(k)
	s.mu.Lock()
	s.invalidated = max(s.invalidated, t)
	delete(s.m, k)
	s.mu.Unlock()
}

// InvalidateFunc drops every entry drop reports. It visits the whole map, so it
// is for writes too rare to index for.
func (m *Map[K, V]) InvalidateFunc(drop func(K, V) bool) {
	t := m.clock.tick()
	for i := range m.shards {
		s := &m.shards[i]
		s.mu.Lock()
		s.invalidated = max(s.invalidated, t)
		maps.DeleteFunc(s.m, func(k K, e entry[V]) bool { return drop(k, e.v) })
		s.mu.Unlock()
	}
}
