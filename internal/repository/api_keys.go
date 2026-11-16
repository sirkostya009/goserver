package repository

import (
	"context"

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

type newAPIKey struct {
	domain.CreateAPIKey

	UserID    domain.ID `barm:"user_id"`
	TokenHash auth.Hash `barm:"token_hash"`
}

const apiKeysTable = "api_keys"

type apiKeyRepository struct{ base }

func (r *apiKeyRepository) Insert(ctx context.Context, userID domain.ID, in domain.CreateAPIKey, hash auth.Hash) (domain.APIKey, error) {
	db := r.handle(ctx)
	row := newAPIKey{CreateAPIKey: in, UserID: userID, TokenHash: hash}
	k, err := db.Insert[newAPIKey]().
		Schema(tenantSchema(ctx)).
		Table(apiKeysTable).
		Values(&row).
		OneAs[domain.APIKey](ctx)
	return k, mapErr(err)
}

func (r *apiKeyRepository) ByHash(ctx context.Context, hash auth.Hash) (domain.APIKey, error) {
	db := r.handle(ctx)
	k, err := db.Select[domain.APIKey]().
		Schema(tenantSchema(ctx)).
		Where("token_hash = ?", hash).
		Where("expires_at IS NULL OR expires_at > now()").
		One(ctx)
	return k, mapErr(err)
}

func (r *apiKeyRepository) ListByUser(ctx context.Context, userID domain.ID) ([]domain.APIKey, error) {
	db := r.handle(ctx)
	keys, err := db.Select[domain.APIKey]().
		Schema(tenantSchema(ctx)).
		Where("user_id = ?", userID).
		OrderBy("id").
		Limit(domain.MaxLimit).
		Slice(ctx)
	return keys, mapErr(err)
}

func (r *apiKeyRepository) Delete(ctx context.Context, id, userID domain.ID) error {
	_, err := r.remove(ctx, id, userID)
	return err
}

// remove is Delete, and the hash of the token it revoked.
func (r *apiKeyRepository) remove(ctx context.Context, id, userID domain.ID) (auth.Hash, error) {
	db := r.handle(ctx)
	hash, err := db.Delete[domain.APIKey]().
		Schema(tenantSchema(ctx)).
		Where("id = ?", id).
		Where("user_id = ?", userID).
		Returning("token_hash").
		OneAs[auth.Hash](ctx)
	return hash, mapErr(err)
}
