package service_test

import (
	"context"
	"errors"
	"iter"
	"slices"
	"strings"
	"testing"
	"uuid"

	"github.com/sirkostya009/ggen"

	"goserver/domain"
)

func TestPosts_Create(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenant("acme")
	guy := f.user(ctx, "Guy", "guy@acme.io")

	p, err := f.Services.Posts.Create(ctx, domain.CreatePost{AuthorID: guy.ID, Title: "hello", Body: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if p.AuthorID != guy.ID || p.Title != "hello" || p.Body != "first" {
		t.Errorf("input not carried over: %+v", p)
	}
	if p.ID == (domain.ID{}) || p.CreatedAt.IsZero() {
		t.Errorf("no generated id or timestamp: %+v", p)
	}
	// the transaction committed, so the post is there for the next reader
	if got, err := f.Services.Posts.Get(ctx, p.ID); err != nil || got != p {
		t.Errorf("get after create: %+v %v", got, err)
	}
}

func TestPosts_Create_AuthorOfAnotherTenant(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme, globex := f.tenant("acme"), f.tenant("globex")
	guy := f.user(acme, "Guy", "guy@acme.io")
	ghost := domain.ID(uuid.NewV7())

	for name, tc := range map[string]struct {
		ctx    context.Context //nolint:containedctx // table-test case data, not a stored context
		author domain.ID
	}{
		"unknown author":           {acme, ghost},
		"author of another tenant": {globex, guy.ID},
	} {
		_, err := f.Services.Posts.Create(tc.ctx, domain.CreatePost{AuthorID: tc.author, Title: "x"})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
		// not found would read as "no such post" to a client
		if errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: ErrNotFound leaked: %v", name, err)
		}
		if err != nil && !strings.Contains(err.Error(), tc.author.String()) {
			t.Errorf("%s: message does not name the author: %v", name, err)
		}
		if posts, _ := f.Services.Posts.List(tc.ctx); len(posts) != 0 {
			t.Errorf("%s: the rejected post was written anyway: %v", name, posts)
		}
	}
}

// Create runs in a transaction of its own, which a caller can put inside one of
// theirs: undoing the outer one undoes the post with it
func TestPosts_Create_NestsInCallersTransaction(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenant("acme")
	guy := f.user(ctx, "Guy", "guy@acme.io")

	outer := ctx
	tx, err := f.Repos.Txer.Begin(&ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.Services.Posts.Create(ctx, domain.CreatePost{AuthorID: guy.ID, Title: "doomed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Services.Posts.Get(ctx, p.ID); err != nil {
		t.Errorf("the post is not visible inside the transaction that wrote it: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	if _, err := f.Services.Posts.Get(outer, p.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the post survived the rollback: %v", err)
	}
}

func TestPosts_List_TenantScopedAndOrdered(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme, globex := f.tenant("acme"), f.tenant("globex")
	guy := f.user(acme, "Guy", "guy@acme.io")
	hank := f.user(acme, "Hank", "hank@acme.io")

	for _, in := range []domain.CreatePost{
		{AuthorID: guy.ID, Title: "first"},
		{AuthorID: hank.ID, Title: "second"},
		{AuthorID: guy.ID, Title: "third"},
	} {
		if _, err := f.Services.Posts.Create(acme, in); err != nil {
			t.Fatal(err)
		}
	}

	all, err := f.Services.Posts.List(acme)
	if err != nil || len(all) != 3 || all[0].Title != "third" || all[2].Title != "first" {
		t.Errorf("list is not newest first: %v %v", all, err)
	}
	if theirs, err := f.Services.Posts.List(globex); err != nil || len(theirs) != 0 {
		t.Errorf("another tenant sees the posts: %v %v", theirs, err)
	}

	mine, err := f.Services.Posts.ListByAuthor(acme, guy.ID)
	if err != nil || len(mine) != 2 || mine[0].Title != "third" || mine[1].Title != "first" {
		t.Errorf("by author: %v %v", mine, err)
	}
	// an author nobody has is a 404, not an empty list
	if _, err := f.Services.Posts.ListByAuthor(acme, domain.ID(uuid.NewV7())); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown author: %v", err)
	}
	if _, err := f.Services.Posts.ListByAuthor(globex, guy.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("author of another tenant: %v", err)
	}
}

func TestPosts_GetAndDelete(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme, globex := f.tenant("acme"), f.tenant("globex")
	guy := f.user(acme, "Guy", "guy@acme.io")

	p, err := f.Services.Posts.Create(acme, domain.CreatePost{AuthorID: guy.ID, Title: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Services.Posts.Get(globex, p.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("another tenant can read the post: %v", err)
	}
	if err := f.Services.Posts.Delete(globex, p.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("another tenant can delete the post: %v", err)
	}
	if err := f.Services.Posts.Delete(acme, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.Services.Posts.Delete(acme, p.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}

// posts belong to their author: deleting the user takes them
func TestPosts_AuthorDeleted_Cascades(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenant("acme")
	guy := f.user(ctx, "Guy", "guy@acme.io")

	if _, err := f.Services.Posts.Create(ctx, domain.CreatePost{AuthorID: guy.ID, Title: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Services.Users.Delete(ctx, guy.ID); err != nil {
		t.Fatal(err)
	}
	if left, err := f.Services.Posts.List(ctx); err != nil || len(left) != 0 {
		t.Errorf("posts survived their author: %v %v", left, err)
	}
}

func TestPosts_Query_Filters(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenant("acme")
	guy := f.user(ctx, "Guy", "guy@acme.io")
	hank := f.user(ctx, "Hank", "hank@acme.io")

	creates := []domain.CreatePost{
		{AuthorID: guy.ID, Title: "alpha", Body: "about go"},
		{AuthorID: hank.ID, Title: "beta", Body: "about rust"},
		{AuthorID: guy.ID, Title: "gamma", Body: "about go again"},
	}
	made := make([]domain.Post, 0, len(creates))
	for _, in := range creates {
		p, err := f.Services.Posts.Create(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		made = append(made, p)
	}

	titles := func(ps []domain.Post) []string {
		out := make([]string, len(ps))
		for i, p := range ps {
			out[i] = p.Title
		}
		return out
	}

	for name, tc := range map[string]struct {
		filter domain.PostFilter
		want   []string
	}{
		"nothing set":     {domain.PostFilter{}, []string{"gamma", "beta", "alpha"}},
		"by id":           {domain.PostFilter{IDs: []domain.ID{made[0].ID}}, []string{"alpha"}},
		"by author":       {domain.PostFilter{AuthorIDs: []domain.ID{guy.ID}}, []string{"gamma", "alpha"}},
		"by two authors":  {domain.PostFilter{AuthorIDs: []domain.ID{guy.ID, hank.ID}}, []string{"gamma", "beta", "alpha"}},
		"title substring": {domain.PostFilter{TitleLike: "amm"}, []string{"gamma"}},
		"body substring":  {domain.PostFilter{BodyLike: "about go"}, []string{"gamma", "alpha"}},
		"author and body": {domain.PostFilter{AuthorIDs: []domain.ID{hank.ID}, BodyLike: "rust"}, []string{"beta"}},
		"oldest first":    {domain.PostFilter{Desc: true}, []string{"alpha", "beta", "gamma"}},
		"by title":        {domain.PostFilter{Sort: domain.PostSortTitle, Desc: true}, []string{"alpha", "beta", "gamma"}},
		"one per page":    {domain.PostFilter{Page: domain.Page{Limit: 1}}, []string{"gamma"}},
		"second page":     {domain.PostFilter{Page: domain.Page{Limit: 1, Offset: 1}}, []string{"beta"}},
	} {
		got, err := f.Services.Posts.Query(ctx, tc.filter)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !slices.Equal(titles(got), tc.want) {
			t.Errorf("%s: got %v, want %v", name, titles(got), tc.want)
		}
	}
}

func TestPosts_Query_TenantScoped(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	acme, globex := f.tenant("acme"), f.tenant("globex")
	guy := f.user(acme, "Guy", "guy@acme.io")
	p, err := f.Services.Posts.Create(acme, domain.CreatePost{AuthorID: guy.ID, Title: "hello"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := f.Services.Posts.Query(globex, domain.PostFilter{IDs: []domain.ID{p.ID}})
	if err != nil || len(got) != 0 {
		t.Errorf("another tenant's post came back: %v %v", got, err)
	}
}

// body is a request body the way the router hands it over: a JSON array
// decoded an element at a time.
func body(json string) iter.Seq2[domain.CreatePost, error] {
	return ggen.NewStream(strings.NewReader(json), nil).Array[domain.CreatePost]()
}

type importFunc = func(context.Context, iter.Seq2[domain.CreatePost, error]) (domain.PostImport, error)

// importModes are both imports, which every import case runs against.
var importModes = map[string]func(fixture) importFunc{
	"batched":   func(f fixture) importFunc { return f.Services.Posts.Import },
	"unbatched": func(f fixture) importFunc { return f.Services.Posts.ImportUnbatched },
}

func TestPosts_Import(t *testing.T) {
	t.Parallel()
	for mode, importer := range importModes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			acme, globex := f.tenant("acme"), f.tenant("globex")
			guy := f.user(acme, "Guy", "guy@acme.io")
			stranger := f.user(globex, "Stranger", "stranger@globex.io")
			ghost := domain.ID(uuid.NewV7())

			got, err := importer(f)(acme, body(`[
				{"author_id":"`+guy.ID.String()+`","title":" first ","body":"one"},
				{"title":"  ","pinned":true},
				{"author_id":"`+ghost.String()+`","title":"ghost"},
				{"author_id":"`+stranger.ID.String()+`","title":"stranger"},
				{"author_id":"`+guy.ID.String()+`","title":"last"}
			]`))
			if err != nil {
				t.Fatal(err)
			}

			if len(got.Posts) != 2 || got.Posts[0].Title != "first" || got.Posts[0].Body != "one" || got.Posts[1].Title != "last" {
				t.Errorf("written %+v", got.Posts)
			}
			if len(got.Failed) != 3 {
				t.Fatalf("failed %+v", got.Failed)
			}
			invalid, ghosted, foreign := got.Failed[0], got.Failed[1], got.Failed[2]
			// every rule it broke, and the post as far as it decoded
			if invalid.Index != 1 || len(invalid.Errors) != 3 || invalid.Post.Title != "" {
				t.Errorf("invalid post reported as %+v", invalid)
			}
			if ghosted.Index != 2 || ghosted.Post.AuthorID != ghost || len(ghosted.Errors) != 1 ||
				!strings.Contains(ghosted.Errors[0], ghost.String()) {
				t.Errorf("ghost author reported as %+v", ghosted)
			}
			if foreign.Index != 3 || foreign.Post.Title != "stranger" {
				t.Errorf("author of another tenant reported as %+v", foreign)
			}

			if stored, err := f.Services.Posts.List(acme); err != nil || len(stored) != 2 {
				t.Errorf("want the two written posts stored, got %+v %v", stored, err)
			}
		})
	}
}

// A body that stops being an array writes nothing, not even what came before.
func TestPosts_Import_Malformed(t *testing.T) {
	t.Parallel()
	for mode, importer := range importModes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			ctx := f.tenant("acme")
			guy := f.user(ctx, "Guy", "guy@acme.io")

			_, err := importer(f)(ctx, body(`[{"author_id":"`+guy.ID.String()+`","title":"a"}, 5]`))
			if !errors.Is(err, domain.ErrMalformed) {
				t.Errorf("want ErrMalformed, got %v", err)
			}
			if posts, _ := f.Services.Posts.List(ctx); len(posts) != 0 {
				t.Errorf("written anyway: %+v", posts)
			}
		})
	}
}

func TestPosts_Import_TooMany(t *testing.T) {
	t.Parallel()
	for mode, importer := range importModes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			ctx := f.tenant("acme")
			guy := f.user(ctx, "Guy", "guy@acme.io")

			post := `{"author_id":"` + guy.ID.String() + `","title":"a"}`
			_, err := importer(f)(ctx, body("["+strings.Repeat(post+",", domain.MaxImport)+post+"]"))
			if !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("want ErrInvalid, got %v", err)
			}
			if posts, _ := f.Services.Posts.List(ctx); len(posts) != 0 {
				t.Errorf("written anyway: %d", len(posts))
			}
		})
	}
}

func TestPosts_Import_Empty(t *testing.T) {
	t.Parallel()
	for mode, importer := range importModes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			got, err := importer(f)(f.tenant("acme"), body(`[]`))
			// lists, not nulls, on the wire
			if err != nil || got.Posts == nil || got.Failed == nil || len(got.Posts)+len(got.Failed) != 0 {
				t.Errorf("%+v %v", got, err)
			}
		})
	}
}
