package domain

import (
	"time"

	"github.com/sirkostya009/barm"
)

// Login is what a session is opened with. The tenant is its slug, the name
// people know it by.
//
//ggen:generate
type Login struct {
	Tenant   string `json:"tenant" pipe:"required @validSlug"`
	Email    string `json:"email" pipe:"required trim tolower maxlen=254"`
	Password string `json:"password" pipe:"required maxlen=1024"`
}

// APIKey is a key as its owner sees it: never the key itself, which is shown
// once, on creation.
//
//ggen:generate
type APIKey struct {
	barm.BaseModel `barm:"table:api_keys,alias:k" json:"-"`

	ID        ID        `barm:"id,pk,default:uuidv7()" json:"id"`
	UserID    ID        `barm:"user_id" json:"user_id"`
	Name      string    `barm:"name" json:"name"`
	CreatedAt time.Time `barm:"created_at,default:now()" json:"created_at"`
	// ExpiresAt is nil for a key that lasts until it is revoked.
	ExpiresAt *time.Time `barm:"expires_at" json:"expires_at"`
}

//ggen:generate
type CreateAPIKey struct {
	Name      string     `barm:"name" json:"name" pipe:"required trim minrunes=1 maxrunes=64"`
	ExpiresAt *time.Time `barm:"expires_at" json:"expires_at"`
}

// NewAPIKey is a key just made, with the one copy of its token there will ever
// be: only its hash is kept.
//
//ggen:generate
type NewAPIKey struct {
	Key   APIKey `json:"key"`
	Token string `json:"token"`
}
