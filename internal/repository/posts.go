package repository

import (
	"context"
	"database/sql"
	"errors"
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
	p, err := insertIfAuthor(db, tenantSchema(ctx), in.Title, in.Body, in.AuthorID).OneAs[domain.Post](ctx)
	return p, mapErr(err)
}

func (r *postRepository) InsertEach(ctx context.Context, in iter.Seq2[domain.CreatePost, error]) ([]domain.Post, error) {
	db := r.handle(ctx)
	// rendered once, each post only binds its values
	t := insertIfAuthor(db, tenantSchema(ctx), barm.Named("title", nil), barm.Named("body", nil), barm.Named("author", nil)).
		TemplateAs[domain.Post]()
	// nothing reaches the database before Run, and the batch is one implicit
	// transaction there, so a failure writes none of it
	b := db.Batch()
	b.Grow(1024)
	rows := make([]*barm.BatchResult[domain.Post], 0, 1024) // 8 kb
	for p, err := range in {
		if err != nil {
			return nil, err
		}
		rows = append(rows, b.OneAs[domain.Post](bindPost(t, &p)))
	}
	if err := b.Run(ctx); err != nil { //nolint:staticcheck // SA4023 misreads Run, which returns nil once the batch ran
		return nil, mapErr(err)
	}
	out := make([]domain.Post, len(rows))
	for i, r := range rows {
		if r.Err() == nil {
			out[i] = r.Value()
		} else if !errors.Is(r.Err(), sql.ErrNoRows) {
			return nil, mapErr(r.Err())
		}
	}
	return out, nil
}

// insertIfAuthor writes a post only when its author exists, so a missing one is
// no row, which fails that post alone rather than the batch a foreign key
// violation would end. The author is locked against a delete until the write
// commits. The values are the post's, or the named arguments of a template.
func insertIfAuthor(db barm.Handle, s schema, title, body, author any) *barm.InsertQuery[domain.CreatePost] {
	sel := db.Select[domain.User]().
		Apply(s.Q).
		ColumnExpr("u.id, ?, ?", title, body).
		Where("u.id = ?", author).
		For("KEY SHARE")
	return db.Insert[domain.CreatePost]().Apply(s.Q).Table(postsTable).Select(sel)
}

func bindPost(t *barm.InsertTemplate[domain.CreatePost, domain.Post], in *domain.CreatePost) *barm.Bound[domain.Post] {
	return t.With(barm.Named("title", in.Title), barm.Named("body", in.Body), barm.Named("author", in.AuthorID))
}

func (r *postRepository) Delete(ctx context.Context, id domain.ID) error {
	db := r.handle(ctx)
	return deleted(db.Delete[domain.Post]().Apply(tenantSchema(ctx).Q).Where("id = ?", id).Exec(ctx))
}
