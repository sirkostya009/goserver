package cache

import (
	"testing"
	"time"
)

func TestMap_FillGet(t *testing.T) {
	t.Parallel()
	var c Clock
	m := New[int, string](&c, 1024, time.Minute)
	if _, ok := m.Get(1); ok {
		t.Fatal("hit on an empty map")
	}
	m.Fill(1, "one", c.Ticket())
	if v, ok := m.Get(1); !ok || v != "one" {
		t.Errorf("got %q %v", v, ok)
	}
	m.Invalidate(1)
	if _, ok := m.Get(1); ok {
		t.Error("hit after invalidate")
	}
}

// a read that began before an invalidation may hold the invalidated row
func TestMap_Fill_AfterInvalidate_Dropped(t *testing.T) {
	t.Parallel()
	var c Clock
	m := New[int, string](&c, 1024, time.Minute)
	ticket := c.Ticket()
	m.Invalidate(1)
	m.Fill(1, "stale", ticket)
	if _, ok := m.Get(1); ok {
		t.Error("stale fill landed")
	}
	m.Fill(1, "fresh", c.Ticket())
	if v, ok := m.Get(1); !ok || v != "fresh" {
		t.Errorf("fresh fill: %q %v", v, ok)
	}
}

// tickets are shared by every map on a clock
func TestMap_Fill_AfterInvalidateFunc_Dropped(t *testing.T) {
	t.Parallel()
	var c Clock
	a := New[int, int](&c, 1024, time.Minute)
	b := New[int, int](&c, 1024, time.Minute)
	for i := range 100 {
		a.Fill(i, i, c.Ticket())
	}
	ticket := c.Ticket()
	a.InvalidateFunc(func(_, v int) bool { return v%2 == 0 })
	for i := range 100 {
		if _, ok := a.Get(i); ok == (i%2 == 0) {
			t.Fatalf("%d: hit %v", i, ok)
		}
	}
	a.Fill(0, 0, ticket)
	if _, ok := a.Get(0); ok {
		t.Error("stale fill landed")
	}
	b.Fill(0, 0, ticket)
	if _, ok := b.Get(0); !ok {
		t.Error("another map's invalidation dropped a fill")
	}
}

func TestMap_Get_Expired(t *testing.T) {
	t.Parallel()
	var c Clock
	m := New[int, int](&c, 1024, time.Nanosecond)
	m.Fill(1, 1, c.Ticket())
	time.Sleep(time.Millisecond)
	if _, ok := m.Get(1); ok {
		t.Error("expired entry hit")
	}
}

func TestMap_Fill_Bounded(t *testing.T) {
	t.Parallel()
	var c Clock
	m := New[int, int](&c, shardCount*4, time.Minute)
	for i := range 10_000 {
		m.Fill(i, i, c.Ticket())
	}
	for i := range m.shards {
		if n := len(m.shards[i].m); n > 4 {
			t.Fatalf("shard %d holds %d", i, n)
		}
	}
}

func BenchmarkMap_Get(b *testing.B) {
	var c Clock
	m := New[[48]byte, [64]byte](&c, 1<<14, time.Minute)
	var k [48]byte
	m.Fill(k, [64]byte{}, c.Ticket())
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.Get(k)
		}
	})
}
