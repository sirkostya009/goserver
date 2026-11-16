package repository

import (
	"context"

	"github.com/sirkostya009/barm"

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

// UserRow is what Insert writes: the input and the hash of its password, never
// the password.
type UserRow struct {
	domain.CreateUser

	PasswordHash string `barm:"password_hash"`
}

// the table domain.User names, for the inserts that go through CreateUser: an
// input carries columns, not a table.
const usersTable = "users"

type userRepository struct{ base }

func (r *userRepository) Query(ctx context.Context, f domain.UserFilter) ([]domain.User, error) {
	users, err := r.handle(ctx).Select[domain.User]().Schema(tenantSchema(ctx)).Apply(f.Q).Slice(ctx)
	return users, mapErr(err)
}

var userByID = qb.Select[domain.User]().Schema(tenantSlot).Where("id = ?", barm.Param("id")).Template[byID]()

func (r *userRepository) Get(ctx context.Context, id domain.ID) (domain.User, error) {
	u, err := userByID.Via(r.handle(ctx)).One(ctx, byID{tenantSchema(ctx), id})
	return u, mapErr(err)
}

type userRowIn struct {
	UserRow

	Schema string `barm:"schema"`
}

var userInsert = qb.Insert[UserRow]().Schema(tenantSlot).Table(usersTable).TemplateAs[userRowIn, domain.User]()

func (r *userRepository) Insert(ctx context.Context, in domain.CreateUser, passwordHash string) (domain.User, error) {
	u, err := userInsert.Via(r.handle(ctx)).One(ctx, userRowIn{UserRow{in, passwordHash}, tenantSchema(ctx)})
	return u, mapErr(err)
}

type byEmail struct {
	Schema string `barm:"schema"`
	Email  string `barm:"email"`
}

var userCredentials = qb.Select[UserCredentials]().Schema(tenantSlot).Where("email = ?", barm.Param("email")).Template[byEmail]()

func (r *userRepository) Credentials(ctx context.Context, email string) (UserCredentials, error) {
	c, err := userCredentials.Via(r.handle(ctx)).One(ctx, byEmail{tenantSchema(ctx), email})
	return c, mapErr(err)
}

var userDelete = qb.Delete[domain.User]().Schema(tenantSlot).Where("id = ?", barm.Param("id")).Template[byID]()

func (r *userRepository) Delete(ctx context.Context, id domain.ID) error {
	return deleted(userDelete.Via(r.handle(ctx)).Exec(ctx, byID{tenantSchema(ctx), id}))
}
