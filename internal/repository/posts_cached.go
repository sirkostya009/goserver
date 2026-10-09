package repository

import (
	"context"
	"iter"

	"goserver/domain"
	"goserver/internal/cache"
)

type cachedPostRepository struct {
	repo  PostRepository
	clock *cache.Clock
	posts *cache.Map[rowKey, domain.Post]
}

func (r *cachedPostRepository) Query(ctx context.Context, f domain.PostFilter) ([]domain.Post, error) {
	return r.repo.Query(ctx, f)
}

func (r *cachedPostRepository) Get(ctx context.Context, id domain.ID) (domain.Post, error) {
	return cached(ctx, r.clock, r.posts, tPosts, rowKeyOf(ctx, id), func() (domain.Post, error) {
		return r.repo.Get(ctx, id)
	})
}

func (r *cachedPostRepository) Insert(ctx context.Context, in domain.CreatePost) (domain.Post, error) {
	p, err := r.repo.Insert(ctx, in)
	wrote(ctx, tPosts, nil)
	return p, err
}

func (r *cachedPostRepository) InsertIfAuthor(ctx context.Context, in domain.CreatePost) (domain.Post, error) {
	p, err := r.repo.InsertIfAuthor(ctx, in)
	wrote(ctx, tPosts, nil)
	return p, err
}

func (r *cachedPostRepository) InsertEach(ctx context.Context, in iter.Seq2[domain.CreatePost, error]) ([]domain.Post, error) {
	ps, err := r.repo.InsertEach(ctx, in)
	wrote(ctx, tPosts, nil)
	return ps, err
}

func (r *cachedPostRepository) Delete(ctx context.Context, id domain.ID) error {
	err := r.repo.Delete(ctx, id)
	k := rowKeyOf(ctx, id)
	wrote(ctx, tPosts, func() { r.posts.Invalidate(k) })
	return err
}
