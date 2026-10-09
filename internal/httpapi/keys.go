package httpapi

import (
	"context"
	"net/http"

	"goserver/domain"
	"goserver/internal/service"
)

//rr:pre @requireTenant
type KeysApi struct {
	Keys service.APIKeyService
}

//rr:route GET /api/keys
func (a *KeysApi) List(ctx context.Context) ([]domain.APIKey, error) {
	return a.Keys.List(ctx)
}

// Create answers with the key's token, which is shown this once.
//
//rr:route POST /api/keys
func (a *KeysApi) Create(w http.ResponseWriter, ctx context.Context, body domain.CreateAPIKey) (domain.NewAPIKey, error) {
	key, err := a.Keys.Create(ctx, body)
	if err != nil {
		return key, err
	}
	w.Header().Set("Cache-Control", "no-store")
	return key, nil
}

//rr:route DELETE /api/keys/{id=@domain.ParseID}
func (a *KeysApi) Revoke(ctx context.Context, id domain.ID) error {
	return a.Keys.Revoke(ctx, id)
}
