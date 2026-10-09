package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/sirkostya009/ggen"

	"goserver/domain"
)

var hello = domain.Post{ID: postID, AuthorID: guyID, Title: "hello", Body: "first", CreatedAt: createdAt}

const helloJSON = `{"author_id":"` + guyText + `","body":"first","created_at":"` + createdAtText +
	`","id":"` + postText + `","title":"hello"}`

func TestPosts_List(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.posts.posts = []domain.Post{hello}

	wantJSON(t, h.do(t, request{method: "GET", path: "/api/posts", tenant: acmeText}), `[`+helloJSON+`]`)
	if h.posts.tenant != acme {
		t.Errorf("tenant %v", h.posts.tenant)
	}
}

func TestPosts_ListByAuthor(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.posts.posts = []domain.Post{hello}

	wantJSON(t, h.do(t, request{method: "GET", path: "/api/users/" + guyText + "/posts", tenant: acmeText}), `[`+helloJSON+`]`)
	if h.posts.tenant != acme || h.posts.authorID != guyID {
		t.Errorf("bound tenant %v author %v", h.posts.tenant, h.posts.authorID)
	}
	// the user routes share the prefix and must not have been hit
	if h.users.id != (domain.ID{}) {
		t.Error("request leaked into the users controller")
	}
}

func TestPosts_Create(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.posts.post = hello

	rec := h.do(t, request{method: "POST", path: "/api/posts", tenant: acmeText,
		body: `{"author_id":"` + guyText + `","title":"  hello ","body":" first "}`})
	wantJSON(t, rec, helloJSON)

	if want := (domain.CreatePost{AuthorID: guyID, Title: "hello", Body: "first"}); h.posts.in != want {
		t.Errorf("service got %+v, want %+v", h.posts.in, want)
	}

	// body is optional
	h = newHarness()
	if rec := h.do(t, request{method: "POST", path: "/api/posts", tenant: acmeText,
		body: `{"author_id":"` + guyText + `","title":"hello"}`}); rec.Code != http.StatusOK {
		t.Errorf("without body: %d %s", rec.Code, rec.Body)
	}
}

func TestPosts_Create_BadBodies(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"missing author": `{"title":"x"}`,
		"missing title":  `{"author_id":"` + guyText + `"}`,
		"blank title":    `{"author_id":"` + guyText + `","title":"  "}`,
		"rfc author id":  `{"author_id":"01a0c091-9024-7c9a-9641-9da47e750ce7","title":"x"}`,
		"short author":   `{"author_id":"abc","title":"x"}`,
		"numeric author": `{"author_id":1,"title":"x"}`,
		"unknown key":    `{"author_id":"` + guyText + `","title":"x","pinned":true}`,
	} {
		h := newHarness()
		rec := h.do(t, request{method: "POST", path: "/api/posts", tenant: acmeText, body: body})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d: %s", name, rec.Code, rec.Body)
		}
		if h.posts.in != (domain.CreatePost{}) {
			t.Errorf("%s: reached the service as %+v", name, h.posts.in)
		}
	}
}

// The body reaches the service as a sequence it reads itself: an element that
// only broke rules comes with its errors, and the ones after it follow.
func TestPosts_Import(t *testing.T) {
	t.Parallel()
	for path, unbatched := range map[string]bool{"/api/posts/import": false, "/api/posts/import/unbatched": true} {
		h := newHarness()
		h.posts.imported = domain.PostImport{
			Posts:  []domain.Post{hello},
			Failed: []domain.FailedPost{{Index: 1, Post: domain.CreatePost{Title: "x"}, Errors: []string{"no author"}}},
		}
		rec := h.do(t, request{method: "POST", path: path, tenant: acmeText,
			body: `[{"author_id":"` + guyText + `","title":" hello ","body":"first"}, {"title":"x","pinned":1}, {"author_id":"` + guyText + `","title":"y"}]`})
		wantJSON(t, rec, `{"failed":[{"errors":["no author"],"index":1,"post":{"author_id":"AAAAAAAAAAAAAAAAAAAAAA","body":"","title":"x"}}],"posts":[`+helloJSON+`]}`)

		if h.posts.tenant != acme || h.posts.unbatched != unbatched {
			t.Errorf("%s: bound tenant %v, unbatched %v", path, h.posts.tenant, h.posts.unbatched)
		}
		want := []domain.CreatePost{{AuthorID: guyID, Title: "hello", Body: "first"}, {Title: "x"}, {AuthorID: guyID, Title: "y"}}
		if fmt.Sprint(h.posts.read) != fmt.Sprint(want) {
			t.Errorf("%s: read %+v, want %+v", path, h.posts.read, want)
		}
		if len(h.posts.readErrs) != 3 || h.posts.readErrs[0] != nil || h.posts.readErrs[2] != nil {
			t.Fatalf("%s: read errors %v", path, h.posts.readErrs)
		}
		// the missing author and the unknown key, both reported
		if es, ok := errors.AsType[ggen.Errors](h.posts.readErrs[1]); !ok || len(es) != 2 {
			t.Errorf("%s: second element's error = %v", path, h.posts.readErrs[1])
		}
	}
}

// A body that is no array is not turned away before the service, which reads
// it, and answers ErrMalformed as rr answers a body it reads itself.
func TestPosts_Import_Malformed(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.posts.err = fmt.Errorf("%w: unexpected end", domain.ErrMalformed)
	rec := h.do(t, request{method: "POST", path: "/api/posts/import", tenant: acmeText, body: `{"title":"x"}`})
	if rec.Code != http.StatusBadRequest || rec.Body.String() != "malformed body: unexpected end\n" {
		t.Errorf("want 400 malformed, got %d %q", rec.Code, rec.Body)
	}
	if len(h.posts.readErrs) != 1 || h.posts.readErrs[0] == nil {
		t.Fatalf("read errors %v, want one", h.posts.readErrs)
	}
	if _, ok := errors.AsType[ggen.Errors](h.posts.readErrs[0]); ok {
		t.Errorf("a parse error read as broken rules: %v", h.posts.readErrs[0])
	}
}

func TestPosts_GetAndDelete(t *testing.T) {
	t.Parallel()
	h := newHarness()
	h.posts.post = hello

	wantJSON(t, h.do(t, request{method: "GET", path: "/api/posts/" + postText, tenant: acmeText}), helloJSON)
	if h.posts.tenant != acme || h.posts.id != postID {
		t.Errorf("get bound tenant %v id %v", h.posts.tenant, h.posts.id)
	}

	h = newHarness()
	rec := h.do(t, request{method: "DELETE", path: "/api/posts/" + postText, tenant: acmeText})
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("want an empty 200, got %d %q", rec.Code, rec.Body)
	}
	if h.posts.tenant != acme || h.posts.id != postID {
		t.Errorf("delete bound tenant %v id %v", h.posts.tenant, h.posts.id)
	}
}
