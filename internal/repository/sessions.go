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

// SessionRow is what Insert writes: the session and the hash of its token,
// never the token.
type SessionRow struct {
	UserID    domain.ID `barm:"user_id"`
	TokenHash auth.Hash `barm:"token_hash"`
	ExpiresAt time.Time `barm:"expires_at"`
}

const sessionsTable = "sessions"

type sessionRepository struct{ base }

type sessionRowIn struct {
	SessionRow

	Schema string `barm:"schema"`
}

var sessionInsert = qb.Insert[SessionRow]().Schema(tenantSlot).Table(sessionsTable).TemplateAs[sessionRowIn, Session]()

func (r *sessionRepository) Insert(ctx context.Context, userID domain.ID, hash auth.Hash, expiresAt time.Time) (Session, error) {
	s, err := sessionInsert.Via(r.handle(ctx)).One(ctx, sessionRowIn{SessionRow{userID, hash, expiresAt}, tenantSchema(ctx)})
	return s, mapErr(err)
}

var sessionByHash = qb.Select[Session]().Schema(tenantSlot).
	Where("token_hash = ?", barm.Param("hash")).
	Where("expires_at > now()").
	Template[byHash]()

func (r *sessionRepository) ByHash(ctx context.Context, hash auth.Hash) (Session, error) {
	s, err := sessionByHash.Via(r.handle(ctx)).One(ctx, byHash{tenantSchema(ctx), hash})
	return s, mapErr(err)
}

func (r *sessionRepository) Delete(ctx context.Context, id domain.ID) error {
	_, err := r.remove(ctx, id)
	return err
}

var sessionsExpired = qb.Delete[Session]().Schema(tenantSlot).
	Where("user_id = ?", barm.Param("user")).
	Where("expires_at <= now()").
	Template[byUser]()

func (r *sessionRepository) DeleteExpired(ctx context.Context, userID domain.ID) error {
	_, err := sessionsExpired.Via(r.handle(ctx)).Exec(ctx, byUser{tenantSchema(ctx), userID})
	return mapErr(err)
}

var sessionRemove = qb.Delete[Session]().Schema(tenantSlot).
	Where("id = ?", barm.Param("id")).
	Returning("token_hash").
	Template[byID]()

// remove is Delete, and the hash of the token it revoked.
func (r *sessionRepository) remove(ctx context.Context, id domain.ID) (auth.Hash, error) {
	hash, err := sessionRemove.Via(r.handle(ctx)).OneAs[auth.Hash](ctx, byID{tenantSchema(ctx), id})
	return hash, mapErr(err)
}
