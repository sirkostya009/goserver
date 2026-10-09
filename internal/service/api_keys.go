package service

import (
	"context"
	"fmt"
	"time"

	"goserver/domain"
	"goserver/internal/auth"
	"goserver/internal/repository"
)

// APIKeyService manages the caller's own keys. Only a session may make or
// revoke one: a key that could make keys would outlive its own revocation.
type APIKeyService interface {
	List(ctx context.Context) ([]domain.APIKey, error)
	Create(ctx context.Context, in domain.CreateAPIKey) (domain.NewAPIKey, error)
	Revoke(ctx context.Context, id domain.ID) error
}

type apiKeyService struct {
	keys repository.APIKeyRepository
}

func NewAPIKeyService(keys repository.APIKeyRepository) APIKeyService {
	return &apiKeyService{keys: keys}
}

func (s *apiKeyService) List(ctx context.Context) ([]domain.APIKey, error) {
	p, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	return s.keys.ListByUser(ctx, p.UserID)
}

func (s *apiKeyService) Create(ctx context.Context, in domain.CreateAPIKey) (domain.NewAPIKey, error) {
	p, err := sessionCaller(ctx)
	if err != nil {
		return domain.NewAPIKey{}, err
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(time.Now()) {
		return domain.NewAPIKey{}, fmt.Errorf("%w: expires_at is in the past", domain.ErrInvalid)
	}
	token, hash := auth.NewToken(auth.APIKey, domain.MustTenant(ctx).ID)
	key, err := s.keys.Insert(ctx, p.UserID, in, hash)
	if err != nil {
		return domain.NewAPIKey{}, err
	}
	return domain.NewAPIKey{Key: key, Token: token}, nil
}

func (s *apiKeyService) Revoke(ctx context.Context, id domain.ID) error {
	p, err := sessionCaller(ctx)
	if err != nil {
		return err
	}
	return s.keys.Delete(ctx, id, p.UserID)
}

func sessionCaller(ctx context.Context) (*domain.Principal, error) {
	p, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	if p.Via != domain.CredentialSession {
		return nil, fmt.Errorf("%w: api keys are managed from a session", domain.ErrForbidden)
	}
	return p, nil
}
