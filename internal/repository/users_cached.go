package repository

import (
	"context"

	"goserver/domain"
	"goserver/internal/cache"
)

type cachedUserRepository struct {
	repo  UserRepository
	clock *cache.Clock
	users *cache.Map[rowKey, domain.User]

	// the maps a user's delete cascades into, shared with their repositories
	posts    *cache.Map[rowKey, domain.Post]
	sessions *cache.Map[tokenKey, Session]
	apiKeys  *cache.Map[tokenKey, domain.APIKey]
}

func (r *cachedUserRepository) Query(ctx context.Context, f domain.UserFilter) ([]domain.User, error) {
	return r.repo.Query(ctx, f)
}

func (r *cachedUserRepository) Get(ctx context.Context, id domain.ID) (domain.User, error) {
	return cached(ctx, r.clock, r.users, tUsers, rowKeyOf(ctx, id), func() (domain.User, error) {
		return r.repo.Get(ctx, id)
	})
}

func (r *cachedUserRepository) Insert(ctx context.Context, in domain.CreateUser, passwordHash string) (domain.User, error) {
	u, err := r.repo.Insert(ctx, in, passwordHash)
	wrote(ctx, tUsers, nil)
	return u, err
}

func (r *cachedUserRepository) Credentials(ctx context.Context, email string) (UserCredentials, error) {
	return r.repo.Credentials(ctx, email)
}

func (r *cachedUserRepository) Delete(ctx context.Context, id domain.ID) error {
	err := r.repo.Delete(ctx, id)
	k := rowKeyOf(ctx, id)
	wrote(ctx, tUsersCascade, func() {
		r.users.Invalidate(k)
		r.posts.InvalidateFunc(func(pk rowKey, p domain.Post) bool { return pk.tenant == k.tenant && p.AuthorID == id })
		r.sessions.InvalidateFunc(func(sk tokenKey, s Session) bool { return sk.tenant == k.tenant && s.UserID == id })
		r.apiKeys.InvalidateFunc(func(kk tokenKey, a domain.APIKey) bool { return kk.tenant == k.tenant && a.UserID == id })
	})
	return err
}
