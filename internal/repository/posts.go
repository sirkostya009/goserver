package repository

import (
	"context"
	"iter"

	"github.com/sirkostya009/barm"

	"goserver/domain"
)

type PostRepository interface {
	// Query lists the posts matching f, page and order included.
	Query(ctx context.Context, f domain.PostFilter) ([]domain.Post, error)
	Get(ctx context.Context, id domain.ID) (domain.Post, error)
	// Insert writes the row and returns it as the database stored it.
	Insert(ctx context.Context, in domain.CreatePost) (domain.Post, error)
	// InsertIfAuthor is Insert of a post whose author may not exist, which is
	// ErrNotFound rather than a foreign key violation.
	InsertIfAuthor(ctx context.Context, in domain.CreatePost) (domain.Post, error)
	// InsertEach is InsertIfAuthor of every post in yields, in one round trip
	// once it ends. The rows come back in the order the posts came, a zero
	// Post where the author does not exist. An error from in ends it and
	// nothing is written.
	InsertEach(ctx context.Context, in iter.Seq2[domain.CreatePost, error]) ([]domain.Post, error)
	Delete(ctx context.Context, id domain.ID) error
}

// the table domain.Post names, see usersTable
const postsTable = "posts"

type postRepository struct{ base }

func (r *postRepository) Query(ctx context.Context, f domain.PostFilter) ([]domain.Post, error) {
	posts, err := r.handle(ctx).Select[domain.Post]().Apply(tenantSchema(ctx).Q, f.Q).Slice(ctx)
	return posts, mapErr(err)
}

func (r *postRepository) Get(ctx context.Context, id domain.ID) (domain.Post, error) {
	db := r.handle(ctx)
	p, err := db.Select[domain.Post]().Apply(tenantSchema(ctx).Q).Where("id = ?", id).One(ctx)
	return p, mapErr(err)
}

func (r *postRepository) Insert(ctx context.Context, in domain.CreatePost) (domain.Post, error) {
	db := r.handle(ctx)
	p, err := db.Insert[domain.CreatePost]().
		Apply(tenantSchema(ctx).Q).
		Table(postsTable).
		Values(&in).
		OneAs[domain.Post](ctx)
	return p, mapErr(err)
}

func (r *postRepository) InsertIfAuthor(ctx context.Context, in domain.CreatePost) (domain.Post, error) {
	db := r.handle(ctx)
	p, err := insertIfAuthor(db, tenantSchema(ctx), &in).OneAs[domain.Post](ctx)
	return p, mapErr(err)
}

func (r *postRepository) InsertEach(ctx context.Context, in iter.Seq2[domain.CreatePost, error]) ([]domain.Post, error) {
	db := r.handle(ctx)
	s := tenantSchema(ctx)
	// nothing reaches the database before Run, and the batch is one implicit
	// transaction there, so a failure writes none of it
	b := db.Batch()
	var rows []*barm.BatchResult[[]domain.Post]
	for p, err := range in {
		if err != nil {
			return nil, err
		}
		rows = append(rows, b.SliceAs[domain.Post](insertIfAuthor(db, s, &p)))
	}
	if err := b.Run(ctx); err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.Post, len(rows))
	for i, r := range rows {
		if v := r.Value(); len(v) > 0 {
			out[i] = v[0]
		}
	}
	return out, nil
}

// insertIfAuthor writes in only when its author exists, so a missing one is no
// rows rather than an error, which would end a batch. The author is locked
// against a delete until the write commits, which the foreign key would fail.
// The SQL is the same for every post of a tenant, so pgx prepares it once per
// connection.
func insertIfAuthor(db barm.Handle, s schema, in *domain.CreatePost) *barm.InsertQuery[domain.CreatePost] {
	author := db.Select[domain.User]().
		Apply(s.Q).
		ColumnExpr("u.id, ?, ?", in.Title, in.Body).
		Where("u.id = ?", in.AuthorID).
		For("KEY SHARE")
	return db.Insert[domain.CreatePost]().Apply(s.Q).Table(postsTable).Select(author)
}

func (r *postRepository) Delete(ctx context.Context, id domain.ID) error {
	db := r.handle(ctx)
	return deleted(db.Delete[domain.Post]().Apply(tenantSchema(ctx).Q).Where("id = ?", id).Exec(ctx))
}
