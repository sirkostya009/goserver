package service

import (
	"context"
	"fmt"

	"goserver/domain"
	"goserver/internal/auth"
	"goserver/internal/repository"
)

type UserService interface {
	List(ctx context.Context) ([]domain.User, error)
	Query(ctx context.Context, f domain.UserFilter) ([]domain.User, error)
	Get(ctx context.Context, id domain.ID) (domain.User, error)
	Create(ctx context.Context, in domain.CreateUser) (domain.User, error)
	Delete(ctx context.Context, id domain.ID) error
}

type userService struct {
	users repository.UserRepository
}

func NewUserService(users repository.UserRepository) UserService {
	return &userService{users: users}
}

// List is the whole list, which is the first page of no filter at all.
func (s *userService) List(ctx context.Context) ([]domain.User, error) {
	return s.users.Query(ctx, domain.UserFilter{})
}

func (s *userService) Query(ctx context.Context, f domain.UserFilter) ([]domain.User, error) {
	return s.users.Query(ctx, f)
}

func (s *userService) Get(ctx context.Context, id domain.ID) (domain.User, error) {
	return s.users.Get(ctx, id)
}

func (s *userService) Create(ctx context.Context, in domain.CreateUser) (domain.User, error) {
	if in.Password == "" {
		return domain.User{}, fmt.Errorf("%w: a user needs a password", domain.ErrInvalid)
	}
	hash, err := auth.HashPassword(ctx, in.Password)
	if err != nil {
		return domain.User{}, err
	}
	return s.users.Insert(ctx, in, hash)
}

func (s *userService) Delete(ctx context.Context, id domain.ID) error {
	return s.users.Delete(ctx, id)
}
