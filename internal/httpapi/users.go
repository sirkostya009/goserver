package httpapi

import (
	"context"

	"goserver/domain"
	"goserver/internal/service"
)

//rr:pre @requireTenant
type UsersApi struct {
	Users service.UserService
}

//rr:route GET /api/users
func (a *UsersApi) List(ctx context.Context) ([]domain.User, error) {
	return a.Users.List(ctx)
}

//rr:route QUERY /api/users
func (a *UsersApi) Query(ctx context.Context, body domain.UserFilter) ([]domain.User, error) {
	return a.Users.Query(ctx, body)
}

//rr:route POST /api/users
func (a *UsersApi) Create(ctx context.Context, body domain.CreateUser) (domain.User, error) {
	return a.Users.Create(ctx, body)
}

//rr:route GET /api/users/{id=@domain.ParseID}
func (a *UsersApi) Get(ctx context.Context, id domain.ID) (domain.User, error) {
	return a.Users.Get(ctx, id)
}

//rr:route DELETE /api/users/{id=@domain.ParseID}
func (a *UsersApi) Delete(ctx context.Context, id domain.ID) error {
	return a.Users.Delete(ctx, id)
}
