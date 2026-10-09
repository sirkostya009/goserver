package repository

import (
	"context"
	"time"

	"goserver/domain"
	"goserver/internal/auth"
	"goserver/internal/cache"
)

// apiKeyStore is an APIKeyRepository that also says which token a delete
// revoked, which is the key the cache drops.
type apiKeyStore interface {
	APIKeyRepository
	remove(ctx context.Context, id, userID domain.ID) (auth.Hash, error)
}

var _ apiKeyStore = (*apiKeyRepository)(nil)

type cachedAPIKeyRepository struct {
	repo    apiKeyStore
	clock   *cache.Clock
	apiKeys *cache.Map[tokenKey, domain.APIKey]
}

func (r *cachedAPIKeyRepository) Insert(ctx context.Context, userID domain.ID, in domain.CreateAPIKey, hash auth.Hash) (domain.APIKey, error) {
	k, err := r.repo.Insert(ctx, userID, in, hash)
	wrote(ctx, tAPIKeys, nil)
	return k, err
}

func (r *cachedAPIKeyRepository) ByHash(ctx context.Context, hash auth.Hash) (domain.APIKey, error) {
	k, err := cached(ctx, r.clock, r.apiKeys, tAPIKeys, tokenKeyOf(ctx, hash), func() (domain.APIKey, error) {
		return r.repo.ByHash(ctx, hash)
	})
	// a cached key outlives its expiry
	if err == nil && k.ExpiresAt != nil && !k.ExpiresAt.After(time.Now()) {
		return domain.APIKey{}, domain.ErrNotFound
	}
	return k, err
}

func (r *cachedAPIKeyRepository) ListByUser(ctx context.Context, userID domain.ID) ([]domain.APIKey, error) {
	return r.repo.ListByUser(ctx, userID)
}

func (r *cachedAPIKeyRepository) Delete(ctx context.Context, id, userID domain.ID) error {
	hash, err := r.repo.remove(ctx, id, userID)
	if err != nil {
		return err
	}
	k := tokenKeyOf(ctx, hash)
	wrote(ctx, tAPIKeys, func() { r.apiKeys.Invalidate(k) })
	return nil
}
