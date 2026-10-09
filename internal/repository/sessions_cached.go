package repository

import (
	"context"
	"time"

	"goserver/domain"
	"goserver/internal/auth"
	"goserver/internal/cache"
)

// sessionStore is a SessionRepository that also says which token a delete
// revoked, which is the key the cache drops.
type sessionStore interface {
	SessionRepository
	remove(ctx context.Context, id domain.ID) (auth.Hash, error)
}

var _ sessionStore = (*sessionRepository)(nil)

type cachedSessionRepository struct {
	repo     sessionStore
	clock    *cache.Clock
	sessions *cache.Map[tokenKey, Session]
}

func (r *cachedSessionRepository) Insert(ctx context.Context, userID domain.ID, hash auth.Hash, expiresAt time.Time) (Session, error) {
	s, err := r.repo.Insert(ctx, userID, hash, expiresAt)
	wrote(ctx, tSessions, nil)
	return s, err
}

func (r *cachedSessionRepository) ByHash(ctx context.Context, hash auth.Hash) (Session, error) {
	s, err := cached(ctx, r.clock, r.sessions, tSessions, tokenKeyOf(ctx, hash), func() (Session, error) {
		return r.repo.ByHash(ctx, hash)
	})
	// a cached session outlives its expiry
	if err == nil && !s.ExpiresAt.After(time.Now()) {
		return Session{}, domain.ErrNotFound
	}
	return s, err
}

func (r *cachedSessionRepository) Delete(ctx context.Context, id domain.ID) error {
	hash, err := r.repo.remove(ctx, id)
	if err != nil {
		return err
	}
	k := tokenKeyOf(ctx, hash)
	wrote(ctx, tSessions, func() { r.sessions.Invalidate(k) })
	return nil
}

// DeleteExpired leaves the cache be: an expired session is refused on a hit.
func (r *cachedSessionRepository) DeleteExpired(ctx context.Context, userID domain.ID) error {
	err := r.repo.DeleteExpired(ctx, userID)
	wrote(ctx, tSessions, nil)
	return err
}
