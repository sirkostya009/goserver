package repository

import (
	"context"

	"goserver/domain"
)

type UserRepository interface {
	// Query lists the users matching f, page and order included.
	Query(ctx context.Context, f domain.UserFilter) ([]domain.User, error)
	Get(ctx context.Context, id domain.ID) (domain.User, error)
	// Insert writes the row and returns it as the database stored it.
	Insert(ctx context.Context, in domain.CreateUser, passwordHash string) (domain.User, error)
	Delete(ctx context.Context, id domain.ID) error
	// Credentials is the user with this address and what they log in with.
	Credentials(ctx context.Context, email string) (UserCredentials, error)
}

// UserCredentials is a user and their password hash, which is nil for a user
// who has none and so cannot log in.
type UserCredentials struct {
	domain.User

	PasswordHash *string `barm:"password_hash"`
}

// newUser is what Insert writes: the input and the hash of its password, never
// the password.
type newUser struct {
	domain.CreateUser

	PasswordHash string `barm:"password_hash"`
}

// the table domain.User names, for the inserts that go through CreateUser: an
// input carries columns, not a table.
const usersTable = "users"

type userRepository struct{ base }

func (r *userRepository) Query(ctx context.Context, f domain.UserFilter) ([]domain.User, error) {
	users, err := r.handle(ctx).Select[domain.User]().Apply(tenantSchema(ctx).Q, f.Q).Slice(ctx)
	return users, mapErr(err)
}

func (r *userRepository) Get(ctx context.Context, id domain.ID) (domain.User, error) {
	db := r.handle(ctx)
	u, err := db.Select[domain.User]().Apply(tenantSchema(ctx).Q).Where("id = ?", id).One(ctx)
	return u, mapErr(err)
}

func (r *userRepository) Insert(ctx context.Context, in domain.CreateUser, passwordHash string) (domain.User, error) {
	db := r.handle(ctx)
	row := newUser{CreateUser: in, PasswordHash: passwordHash}
	u, err := db.Insert[newUser]().
		Apply(tenantSchema(ctx).Q).
		Table(usersTable).
		Values(&row).
		OneAs[domain.User](ctx)
	return u, mapErr(err)
}

func (r *userRepository) Credentials(ctx context.Context, email string) (UserCredentials, error) {
	db := r.handle(ctx)
	c, err := db.Select[UserCredentials]().Apply(tenantSchema(ctx).Q).Where("email = ?", email).One(ctx)
	return c, mapErr(err)
}

func (r *userRepository) Delete(ctx context.Context, id domain.ID) error {
	db := r.handle(ctx)
	return deleted(db.Delete[domain.User]().Apply(tenantSchema(ctx).Q).Where("id = ?", id).Exec(ctx))
}
