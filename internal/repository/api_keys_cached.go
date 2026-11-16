package repository

import (
	"context"
	"time"

	"goserver/domain"
	"goserver/internal/auth"
)

// apiKeyStore is an APIKeyRepository that also says which token a delete
// revoked, which is the key the cache drops.
type apiKeyStore interface {
	APIKeyRepository
	remove(ctx context.Context, id, userID domain.ID) (auth.Hash, error)
}

var _ apiKeyStore = (*apiKeyRepository)(nil)

type cachedAPIKeyRepository struct {
	apiKeyStore

	cache *tableCache[tokenKey, domain.APIKey]
}

func (r *cachedAPIKeyRepository) Insert(ctx context.Context, userID domain.ID, in domain.CreateAPIKey, hash auth.Hash) (domain.APIKey, error) {
	k, err := r.apiKeyStore.Insert(ctx, userID, in, hash)
	r.cache.wrote(ctx)
	return k, err
}

func (r *cachedAPIKeyRepository) ByHash(ctx context.Context, hash auth.Hash) (domain.APIKey, error) {
	k, err := r.cache.get(ctx, tokenKeyOf(ctx, hash), func() (domain.APIKey, error) {
		return r.apiKeyStore.ByHash(ctx, hash)
	})
	// a cached key outlives its expiry
	if err == nil && k.ExpiresAt != nil && !k.ExpiresAt.After(time.Now()) {
		return domain.APIKey{}, domain.ErrNotFound
	}
	return k, err
}

func (r *cachedAPIKeyRepository) Delete(ctx context.Context, id, userID domain.ID) error {
	hash, err := r.remove(ctx, id, userID)
	if err != nil {
		return err
	}
	r.cache.drop(ctx, tokenKeyOf(ctx, hash))
	return nil
}
