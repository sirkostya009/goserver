package repository

import (
	"context"

	"github.com/sirkostya009/barm"

	"goserver/domain"
	"goserver/internal/auth"
)

type APIKeyRepository interface {
	Insert(ctx context.Context, userID domain.ID, in domain.CreateAPIKey, hash auth.Hash) (domain.APIKey, error)
	// ByHash is the unexpired key with this hash.
	ByHash(ctx context.Context, hash auth.Hash) (domain.APIKey, error)
	// ListByUser is the user's keys, oldest first, at most domain.MaxLimit.
	ListByUser(ctx context.Context, userID domain.ID) ([]domain.APIKey, error)
	// Delete removes the key if userID owns it, and is ErrNotFound otherwise.
	Delete(ctx context.Context, id, userID domain.ID) error
}

// APIKeyRow is what Insert writes: the input, its owner and the hash of its
// token, never the token.
type APIKeyRow struct {
	domain.CreateAPIKey

	UserID    domain.ID `barm:"user_id"`
	TokenHash auth.Hash `barm:"token_hash"`
}

const apiKeysTable = "api_keys"

type byHash struct {
	Schema string    `barm:"schema"`
	Hash   auth.Hash `barm:"hash"`
}

type byUser struct {
	Schema string    `barm:"schema"`
	UserID domain.ID `barm:"user"`
}

type apiKeyRepository struct{ base }

type apiKeyRowIn struct {
	APIKeyRow

	Schema string `barm:"schema"`
}

var apiKeyInsert = qb.Insert[APIKeyRow]().Schema(tenantSlot).Table(apiKeysTable).TemplateAs[apiKeyRowIn, domain.APIKey]()

func (r *apiKeyRepository) Insert(ctx context.Context, userID domain.ID, in domain.CreateAPIKey, hash auth.Hash) (domain.APIKey, error) {
	k, err := apiKeyInsert.Via(r.handle(ctx)).One(ctx, apiKeyRowIn{APIKeyRow{in, userID, hash}, tenantSchema(ctx)})
	return k, mapErr(err)
}

var apiKeyByHash = qb.Select[domain.APIKey]().Schema(tenantSlot).
	Where("token_hash = ?", barm.Param("hash")).
	Where("expires_at IS NULL OR expires_at > now()").
	Template[byHash]()

func (r *apiKeyRepository) ByHash(ctx context.Context, hash auth.Hash) (domain.APIKey, error) {
	k, err := apiKeyByHash.Via(r.handle(ctx)).One(ctx, byHash{tenantSchema(ctx), hash})
	return k, mapErr(err)
}

var apiKeysOfUser = qb.Select[domain.APIKey]().Schema(tenantSlot).
	Where("user_id = ?", barm.Param("user")).
	OrderBy("id").
	Limit(domain.MaxLimit).
	Template[byUser]()

func (r *apiKeyRepository) ListByUser(ctx context.Context, userID domain.ID) ([]domain.APIKey, error) {
	keys, err := apiKeysOfUser.Via(r.handle(ctx)).Slice(ctx, byUser{tenantSchema(ctx), userID})
	return keys, mapErr(err)
}

func (r *apiKeyRepository) Delete(ctx context.Context, id, userID domain.ID) error {
	_, err := r.remove(ctx, id, userID)
	return err
}

type keyOfUser struct {
	Schema string    `barm:"schema"`
	ID     domain.ID `barm:"id"`
	UserID domain.ID `barm:"user"`
}

var apiKeyRemove = qb.Delete[domain.APIKey]().Schema(tenantSlot).
	Where("id = ?", barm.Param("id")).
	Where("user_id = ?", barm.Param("user")).
	Returning("token_hash").
	Template[keyOfUser]()

// remove is Delete, and the hash of the token it revoked.
func (r *apiKeyRepository) remove(ctx context.Context, id, userID domain.ID) (auth.Hash, error) {
	hash, err := apiKeyRemove.Via(r.handle(ctx)).OneAs[auth.Hash](ctx, keyOfUser{tenantSchema(ctx), id, userID})
	return hash, mapErr(err)
}
