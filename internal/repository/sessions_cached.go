package repository

import (
	"context"
	"time"

	"goserver/domain"
	"goserver/internal/auth"
)

// sessionStore is a SessionRepository that also says which token a delete
// revoked, which is the key the cache drops.
type sessionStore interface {
	SessionRepository
	remove(ctx context.Context, id domain.ID) (auth.Hash, error)
}

var _ sessionStore = (*sessionRepository)(nil)

type cachedSessionRepository struct {
	sessionStore

	cache *tableCache[tokenKey, Session]
}

func (r *cachedSessionRepository) Insert(ctx context.Context, userID domain.ID, hash auth.Hash, expiresAt time.Time) (Session, error) {
	s, err := r.sessionStore.Insert(ctx, userID, hash, expiresAt)
	r.cache.wrote(ctx)
	return s, err
}

func (r *cachedSessionRepository) ByHash(ctx context.Context, hash auth.Hash) (Session, error) {
	s, err := r.cache.get(ctx, tokenKeyOf(ctx, hash), func() (Session, error) {
		return r.sessionStore.ByHash(ctx, hash)
	})
	// a cached session outlives its expiry
	if err == nil && !s.ExpiresAt.After(time.Now()) {
		return Session{}, domain.ErrNotFound
	}
	return s, err
}

func (r *cachedSessionRepository) Delete(ctx context.Context, id domain.ID) error {
	hash, err := r.remove(ctx, id)
	if err != nil {
		return err
	}
	r.cache.drop(ctx, tokenKeyOf(ctx, hash))
	return nil
}

// DeleteExpired leaves the cache be: an expired session is refused on a hit.
func (r *cachedSessionRepository) DeleteExpired(ctx context.Context, userID domain.ID) error {
	err := r.sessionStore.DeleteExpired(ctx, userID)
	r.cache.wrote(ctx)
	return err
}
