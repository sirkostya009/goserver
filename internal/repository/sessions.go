package repository

import (
	"context"
	"time"

	"github.com/sirkostya009/barm"

	"goserver/domain"
	"goserver/internal/auth"
)

type SessionRepository interface {
	Insert(ctx context.Context, userID domain.ID, hash auth.Hash, expiresAt time.Time) (Session, error)
	// ByHash is the unexpired session the token with this hash opened.
	ByHash(ctx context.Context, hash auth.Hash) (Session, error)
	Delete(ctx context.Context, id domain.ID) error
	// DeleteExpired drops the user's sessions that can no longer be presented.
	DeleteExpired(ctx context.Context, userID domain.ID) error
}

// Session is a login. It never goes on the wire: the client holds the token,
// the database its hash.
type Session struct {
	barm.BaseModel `barm:"table:sessions,alias:s"`

	ID        domain.ID `barm:"id,pk,default:uuidv7()"`
	UserID    domain.ID `barm:"user_id"`
	CreatedAt time.Time `barm:"created_at,default:now()"`
	ExpiresAt time.Time `barm:"expires_at"`
}

type newSession struct {
	UserID    domain.ID `barm:"user_id"`
	TokenHash auth.Hash `barm:"token_hash"`
	ExpiresAt time.Time `barm:"expires_at"`
}

const sessionsTable = "sessions"

type sessionRepository struct{ base }

func (r *sessionRepository) Insert(ctx context.Context, userID domain.ID, hash auth.Hash, expiresAt time.Time) (Session, error) {
	db := r.handle(ctx)
	row := newSession{UserID: userID, TokenHash: hash, ExpiresAt: expiresAt}
	s, err := db.Insert[newSession]().
		Schema(tenantSchema(ctx)).
		Table(sessionsTable).
		Values(&row).
		OneAs[Session](ctx)
	return s, mapErr(err)
}

func (r *sessionRepository) ByHash(ctx context.Context, hash auth.Hash) (Session, error) {
	db := r.handle(ctx)
	s, err := db.Select[Session]().
		Schema(tenantSchema(ctx)).
		Where("token_hash = ?", hash).
		Where("expires_at > now()").
		One(ctx)
	return s, mapErr(err)
}

func (r *sessionRepository) Delete(ctx context.Context, id domain.ID) error {
	_, err := r.remove(ctx, id)
	return err
}

func (r *sessionRepository) DeleteExpired(ctx context.Context, userID domain.ID) error {
	db := r.handle(ctx)
	_, err := db.Delete[Session]().
		Schema(tenantSchema(ctx)).
		Where("user_id = ?", userID).
		Where("expires_at <= now()").
		Exec(ctx)
	return mapErr(err)
}

// remove is Delete, and the hash of the token it revoked.
func (r *sessionRepository) remove(ctx context.Context, id domain.ID) (auth.Hash, error) {
	db := r.handle(ctx)
	hash, err := db.Delete[Session]().
		Schema(tenantSchema(ctx)).
		Where("id = ?", id).
		Returning("token_hash").
		OneAs[auth.Hash](ctx)
	return hash, mapErr(err)
}
