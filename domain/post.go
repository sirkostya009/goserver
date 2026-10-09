package domain

import (
	"time"

	"github.com/sirkostya009/barm"
)

//ggen:generate
type Post struct {
	barm.BaseModel `barm:"table:posts,alias:p" json:"-"`

	ID        ID        `barm:"id,pk,default:uuidv7()" json:"id"`
	AuthorID  ID        `barm:"author_id" json:"author_id"`
	Title     string    `barm:"title" json:"title"`
	Body      string    `barm:"body" json:"body"`
	CreatedAt time.Time `barm:"created_at,default:now()" json:"created_at"`
}

// CreatePost is a Post without the columns the database fills in: barm inserts
// it and hands back the full row. It names no table, the repository does.
//
//ggen:generate multierr
type CreatePost struct {
	AuthorID ID     `barm:"author_id" json:"author_id" pipe:"required"`
	Title    string `barm:"title" json:"title" pipe:"required trim minrunes=1 maxrunes=200"`
	Body     string `barm:"body" json:"body" pipe:"trim maxrunes=10000"`
}

// MaxImport is the most posts one import takes. Every one of them rides in a
// single batch, which is held in memory until the array ends.
const MaxImport = 10_000

// PostImport is the outcome of an import: the posts it wrote, in the order they
// came, and the ones it could not.
//
//ggen:generate
type PostImport struct {
	Posts  []Post       `json:"posts"`
	Failed []FailedPost `json:"failed"`
}

// FailedPost is a post an import did not write: where it was in the array, the
// post as it decoded, and every reason it was turned away.
//
//ggen:generate
type FailedPost struct {
	Index  int        `json:"index"`
	Post   CreatePost `json:"post"`
	Errors []string   `json:"errors"`
}
