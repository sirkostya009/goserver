package repository

import (
	"context"
	"errors"
	"iter"
	"sync"
	"testing"
	"uuid"

	"github.com/sirkostya009/barm"

	"goserver/domain"
)

// queries records what reaches the database once recording starts.
type queries struct {
	mu     sync.Mutex
	on     bool
	events []barm.QueryEvent
}

func (q *queries) hook() barm.Option {
	return barm.WithHook(barm.QueryHook{AfterQuery: func(_ context.Context, ev *barm.QueryEvent) {
		q.mu.Lock()
		defer q.mu.Unlock()
		if q.on {
			q.events = append(q.events, *ev)
		}
	}})
}

func (q *queries) record() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.on, q.events = true, nil
}

func (q *queries) recorded() []barm.QueryEvent {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.on = false
	return q.events
}

func seqOf(posts ...domain.CreatePost) iter.Seq2[domain.CreatePost, error] {
	return func(yield func(domain.CreatePost, error) bool) {
		for _, p := range posts {
			if !yield(p, nil) {
				return
			}
		}
	}
}

func TestPosts_InsertEach_OneRoundTrip(t *testing.T) {
	t.Parallel()
	var q queries
	f := setupWith(t, []barm.Option{q.hook()}, "acme")
	ctx := as(f.ids["acme"])
	guy, err := f.users.Insert(ctx, domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}
	ghost := domain.ID(uuid.NewV7())

	q.record()
	posts, err := f.posts.InsertEach(ctx, seqOf(
		domain.CreatePost{AuthorID: guy.ID, Title: "a"},
		domain.CreatePost{AuthorID: ghost, Title: "b"},
		domain.CreatePost{AuthorID: guy.ID, Title: "c", Body: "see"},
	))
	events := q.recorded()
	if err != nil {
		t.Fatal(err)
	}

	if len(posts) != 3 || posts[0].Title != "a" || posts[1] != (domain.Post{}) || posts[2].Title != "c" || posts[2].Body != "see" {
		t.Fatalf("want a, a zero post for the ghost, c: %+v", posts)
	}
	if posts[0].ID == (domain.ID{}) || posts[0].AuthorID != guy.ID || posts[0].CreatedAt.IsZero() {
		t.Errorf("not the stored row: %+v", posts[0])
	}
	if len(events) != 3 {
		t.Fatalf("want 3 queries, got %d", len(events))
	}
	for i, ev := range events {
		if ev.BatchSize != 3 || int(ev.BatchIndex) != i || ev.Op != "INSERT" {
			t.Errorf("query %d is not the batch's insert: %s %d/%d", i, ev.Op, ev.BatchIndex, ev.BatchSize)
		}
	}

	stored, err := f.posts.Query(ctx, domain.PostFilter{})
	if err != nil || len(stored) != 2 {
		t.Errorf("want the two posts with an author stored, got %d: %v", len(stored), err)
	}
}

// An error from the sequence ends it before anything was sent.
func TestPosts_InsertEach_SequenceError(t *testing.T) {
	t.Parallel()
	var q queries
	f := setupWith(t, []barm.Option{q.hook()}, "acme")
	ctx := as(f.ids["acme"])
	guy, err := f.users.Insert(ctx, domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}

	broken := errors.New("broken body")
	in := func(yield func(domain.CreatePost, error) bool) {
		if yield(domain.CreatePost{AuthorID: guy.ID, Title: "a"}, nil) {
			yield(domain.CreatePost{}, broken)
		}
	}
	q.record()
	if _, err := f.posts.InsertEach(ctx, in); !errors.Is(err, broken) {
		t.Errorf("want the sequence's error, got %v", err)
	}
	if events := q.recorded(); len(events) != 0 {
		t.Errorf("%d queries reached the database", len(events))
	}
	if stored, _ := f.posts.Query(ctx, domain.PostFilter{}); len(stored) != 0 {
		t.Errorf("written anyway: %v", stored)
	}
}

func TestPosts_InsertEach_Empty(t *testing.T) {
	t.Parallel()
	f := setup(t, "acme")
	posts, err := f.posts.InsertEach(as(f.ids["acme"]), seqOf())
	if err != nil || len(posts) != 0 {
		t.Errorf("want nothing, got %v %v", posts, err)
	}
}

func TestPosts_InsertIfAuthor(t *testing.T) {
	t.Parallel()
	f := setup(t, "acme", "globex")
	acme, globex := as(f.ids["acme"]), as(f.ids["globex"])
	guy, err := f.users.Insert(acme, domain.CreateUser{Name: "Guy", Email: "guy@acme.io"}, "")
	if err != nil {
		t.Fatal(err)
	}

	p, err := f.posts.InsertIfAuthor(acme, domain.CreatePost{AuthorID: guy.ID, Title: "a"})
	if err != nil || p.AuthorID != guy.ID || p.Title != "a" {
		t.Errorf("insert: %+v %v", p, err)
	}
	// another tenant's user is no author here
	if _, err := f.posts.InsertIfAuthor(globex, domain.CreatePost{AuthorID: guy.ID, Title: "b"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("author of another tenant: want ErrNotFound, got %v", err)
	}
	if stored, _ := f.posts.Query(acme, domain.PostFilter{}); len(stored) != 1 || stored[0].Title != "a" {
		t.Errorf("stored %+v", stored)
	}
}
