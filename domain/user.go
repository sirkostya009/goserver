package domain

//go:generate go tool ggen .

import (
	"time"

	"github.com/sirkostya009/barm"
)

//ggen:generate
type User struct {
	barm.BaseModel `barm:"table:users,alias:u" json:"-"`

	ID        ID        `barm:"id,pk,default:uuidv7()" json:"id"`
	Name      string    `barm:"name" json:"name"`
	Email     string    `barm:"email" json:"email"`
	CreatedAt time.Time `barm:"created_at,default:now()" json:"created_at"`
}

// CreateUser is a User without the columns the database fills in: barm inserts
// it and hands back the full row. It names no table, the repository does.
//
//ggen:generate
type CreateUser struct {
	Name string `barm:"name" json:"name" pipe:"required trim minrunes=1 maxrunes=64"`
	// an address is capped in octets (RFC 5321), so this one stays a byte length
	Email string `barm:"email" json:"email" pipe:"required trim tolower minlen=3 maxlen=254 contains=@"`
	// Password is never stored, only its hash. It is capped in octets because
	// that is what hashing it costs, and never trimmed: every byte is the
	// user's.
	Password string `json:"password" pipe:"required minrunes=8 maxlen=1024"`
}
