package repository

import (
	"context"

	"goserver/domain"
)

type cachedUserRepository struct {
	UserRepository

	cache *tableCache[rowKey, domain.User]

	// the caches a user's delete cascades into, shared with their repositories
	posts    *tableCache[rowKey, domain.Post]
	sessions *tableCache[tokenKey, Session]
	apiKeys  *tableCache[tokenKey, domain.APIKey]
}

func (r *cachedUserRepository) Get(ctx context.Context, id domain.ID) (domain.User, error) {
	return r.cache.get(ctx, rowKeyOf(ctx, id), func() (domain.User, error) { return r.UserRepository.Get(ctx, id) })
}

func (r *cachedUserRepository) Insert(ctx context.Context, in domain.CreateUser, passwordHash string) (domain.User, error) {
	u, err := r.UserRepository.Insert(ctx, in, passwordHash)
	r.cache.wrote(ctx)
	return u, err
}

func (r *cachedUserRepository) Delete(ctx context.Context, id domain.ID) error {
	err := r.UserRepository.Delete(ctx, id)
	k := rowKeyOf(ctx, id)
	r.cache.drop(ctx, k)
	r.posts.dropFunc(ctx, func(pk rowKey, p domain.Post) bool { return pk.tenant == k.tenant && p.AuthorID == id })
	r.sessions.dropFunc(ctx, func(sk tokenKey, s Session) bool { return sk.tenant == k.tenant && s.UserID == id })
	r.apiKeys.dropFunc(ctx, func(kk tokenKey, a domain.APIKey) bool { return kk.tenant == k.tenant && a.UserID == id })
	return err
}
