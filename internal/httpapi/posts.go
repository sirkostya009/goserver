package httpapi

import (
	"context"
	"iter"

	"goserver/domain"
	"goserver/internal/service"
)

//rr:pre @requireTenant
type PostsApi struct {
	Posts service.PostService
}

//rr:route GET /api/posts
func (a *PostsApi) List(ctx context.Context) ([]domain.Post, error) {
	return a.Posts.List(ctx)
}

//rr:route QUERY /api/posts
func (a *PostsApi) Query(ctx context.Context, body domain.PostFilter) ([]domain.Post, error) {
	return a.Posts.Query(ctx, body)
}

//rr:route POST /api/posts
func (a *PostsApi) Create(ctx context.Context, body domain.CreatePost) (domain.Post, error) {
	return a.Posts.Create(ctx, body)
}

// Import writes a JSON array of posts in one round trip, reading it an element
// at a time. Posts that break a rule or name an author that does not exist
// come back as failed, the rest are written.
//
//rr:route POST /api/posts/import
func (a *PostsApi) Import(ctx context.Context, body iter.Seq2[domain.CreatePost, error]) (domain.PostImport, error) {
	return a.Posts.Import(ctx, body)
}

// ImportUnbatched is Import reading the whole array first, then writing a post
// per round trip.
//
//rr:route POST /api/posts/import/unbatched
func (a *PostsApi) ImportUnbatched(ctx context.Context, body iter.Seq2[domain.CreatePost, error]) (domain.PostImport, error) {
	return a.Posts.ImportUnbatched(ctx, body)
}

//rr:route GET /api/posts/{id=@domain.ParseID}
func (a *PostsApi) Get(ctx context.Context, id domain.ID) (domain.Post, error) {
	return a.Posts.Get(ctx, id)
}

//rr:route DELETE /api/posts/{id=@domain.ParseID}
func (a *PostsApi) Delete(ctx context.Context, id domain.ID) error {
	return a.Posts.Delete(ctx, id)
}

//rr:route GET /api/users/{id=@domain.ParseID}/posts
func (a *PostsApi) ListByAuthor(ctx context.Context, id domain.ID) ([]domain.Post, error) {
	return a.Posts.ListByAuthor(ctx, id)
}
