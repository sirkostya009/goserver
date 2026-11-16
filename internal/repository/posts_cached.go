package repository

import (
	"context"
	"iter"

	"goserver/domain"
)

type cachedPostRepository struct {
	PostRepository

	cache *tableCache[rowKey, domain.Post]
}

func (r *cachedPostRepository) Get(ctx context.Context, id domain.ID) (domain.Post, error) {
	return r.cache.get(ctx, rowKeyOf(ctx, id), func() (domain.Post, error) { return r.PostRepository.Get(ctx, id) })
}

func (r *cachedPostRepository) Insert(ctx context.Context, in domain.CreatePost) (domain.Post, error) {
	p, err := r.PostRepository.Insert(ctx, in)
	r.cache.wrote(ctx)
	return p, err
}

func (r *cachedPostRepository) InsertIfAuthor(ctx context.Context, in domain.CreatePost) (domain.Post, error) {
	p, err := r.PostRepository.InsertIfAuthor(ctx, in)
	r.cache.wrote(ctx)
	return p, err
}

func (r *cachedPostRepository) InsertEach(ctx context.Context, in iter.Seq2[domain.CreatePost, error]) ([]domain.Post, error) {
	ps, err := r.PostRepository.InsertEach(ctx, in)
	r.cache.wrote(ctx)
	return ps, err
}

func (r *cachedPostRepository) Delete(ctx context.Context, id domain.ID) error {
	err := r.PostRepository.Delete(ctx, id)
	r.cache.drop(ctx, rowKeyOf(ctx, id))
	return err
}
