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
	posts, err := r.handle(ctx).Select[domain.Post]().Schema(tenantSchema(ctx)).Apply(f.Q).Slice(ctx)
	return posts, mapErr(err)
}

var postByID = qb.Select[domain.Post]().Schema(tenantSlot).Where("id = ?", barm.Param("id")).Template[byID]()

func (r *postRepository) Get(ctx context.Context, id domain.ID) (domain.Post, error) {
	p, err := postByID.Via(r.handle(ctx)).One(ctx, byID{tenantSchema(ctx), id})
	return p, mapErr(err)
}

type postIn struct {
	domain.CreatePost

	Schema string `barm:"schema"`
}

var postInsert = qb.Insert[domain.CreatePost]().Schema(tenantSlot).Table(postsTable).TemplateAs[postIn, domain.Post]()

func (r *postRepository) Insert(ctx context.Context, in domain.CreatePost) (domain.Post, error) {
	p, err := postInsert.Via(r.handle(ctx)).One(ctx, postIn{in, tenantSchema(ctx)})
	return p, mapErr(err)
}

// insertIfAuthor writes a post only when its author exists, so a missing one is
// no row, which fails that post alone rather than the batch a foreign key
// violation would end. The author is locked against a delete until the write
// commits. A batch splices the schema once while it repeats.
var insertIfAuthor = qb.Insert[domain.CreatePost]().Schema(tenantSlot).Table(postsTable).Select(
	qb.Select[domain.User]().
		Schema(tenantSlot).
		ColumnExpr("u.id, ?, ?", barm.Param("title"), barm.Param("body")).
		Where("u.id = ?", barm.Param("author_id")).
		For("KEY SHARE"),
).TemplateAs[postIn, domain.Post]()

func (r *postRepository) InsertIfAuthor(ctx context.Context, in domain.CreatePost) (domain.Post, error) {
	p, err := insertIfAuthor.Via(r.handle(ctx)).One(ctx, postIn{in, tenantSchema(ctx)})
	return p, mapErr(err)
}

func (r *postRepository) InsertEach(ctx context.Context, in iter.Seq2[domain.CreatePost, error]) ([]domain.Post, error) {
	db := r.handle(ctx)
	s := tenantSchema(ctx)
	// nothing reaches the database before Run, and the batch is one implicit
	// transaction there, so a failure writes none of it
	b := db.Batch()
	b.Grow(1024)
	q := insertIfAuthor.In(b)
	rows := make([]*barm.BatchResult[domain.Post], 0, 1024) // 8 kb
	for p, err := range in {
		if err != nil {
			return nil, err
		}
		rows = append(rows, q.One(postIn{p, s}))
	}
	if err := b.Run(ctx); err != nil {
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

var postDelete = qb.Delete[domain.Post]().Schema(tenantSlot).Where("id = ?", barm.Param("id")).Template[byID]()

func (r *postRepository) Delete(ctx context.Context, id domain.ID) error {
	return deleted(postDelete.Via(r.handle(ctx)).Exec(ctx, byID{tenantSchema(ctx), id}))
}
