package service

import "goserver/internal/repository"

// Container is every service there is, built on the data layer below it.
type Container struct {
	Tenants TenantService
	Users   UserService
	Posts   PostService
	Auth    AuthService
	APIKeys APIKeyService
}

// New wires the service layer over repos. It does no I/O.
func New(repos repository.Container) Container {
	return Container{
		Tenants: NewTenantService(repos.Tenants),
		Users:   NewUserService(repos.Users),
		Posts:   NewPostService(repos.Posts, repos.Users, repos.Txer),
		Auth:    NewAuthService(repos.Tenants, repos.Users, repos.Sessions, repos.APIKeys),
		APIKeys: NewAPIKeyService(repos.APIKeys),
	}
}
