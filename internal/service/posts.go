package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"

	"github.com/sirkostya009/ggen"

	"goserver/domain"
	"goserver/internal/repository"
)

type PostService interface {
	List(ctx context.Context) ([]domain.Post, error)
	Query(ctx context.Context, f domain.PostFilter) ([]domain.Post, error)
	ListByAuthor(ctx context.Context, authorID domain.ID) ([]domain.Post, error)
	Get(ctx context.Context, id domain.ID) (domain.Post, error)
	Create(ctx context.Context, in domain.CreatePost) (domain.Post, error)
	// Import writes the posts of a JSON array as in decodes them, in one round
	// trip once the array ends. A post that breaks a rule, or whose author does
	// not exist, is reported rather than written. A malformed array writes
	// nothing.
	Import(ctx context.Context, in iter.Seq2[domain.CreatePost, error]) (domain.PostImport, error)
	// ImportUnbatched is Import reading the whole array before it writes, and
	// writing a post per round trip.
	ImportUnbatched(ctx context.Context, in iter.Seq2[domain.CreatePost, error]) (domain.PostImport, error)
	Delete(ctx context.Context, id domain.ID) error
}

type postService struct {
	posts repository.PostRepository
	users repository.UserRepository
	txer  repository.Txer
}

func NewPostService(posts repository.PostRepository, users repository.UserRepository, txer repository.Txer) PostService {
	return &postService{posts: posts, users: users, txer: txer}
}

// List is the whole list, which is the first page of no filter at all.
func (s *postService) List(ctx context.Context) ([]domain.Post, error) {
	return s.posts.Query(ctx, domain.PostFilter{})
}

func (s *postService) Query(ctx context.Context, f domain.PostFilter) ([]domain.Post, error) {
	return s.posts.Query(ctx, f)
}

// ListByAuthor says whether the author exists, which a filter on its own
// cannot: no posts and no such author read the same otherwise.
func (s *postService) ListByAuthor(ctx context.Context, authorID domain.ID) ([]domain.Post, error) {
	if _, err := s.users.Get(ctx, authorID); err != nil {
		return nil, err
	}
	return s.posts.Query(ctx, domain.PostFilter{AuthorIDs: []domain.ID{authorID}})
}

func (s *postService) Get(ctx context.Context, id domain.ID) (domain.Post, error) {
	return s.posts.Get(ctx, id)
}

// Create reads the author and writes the post in one transaction, so the post
// cannot land against an author deleted between the two.
func (s *postService) Create(ctx context.Context, in domain.CreatePost) (domain.Post, error) {
	// Begin points ctx at the transaction, so the repositories below run in it
	tx, err := s.txer.Begin(&ctx, nil)
	if err != nil {
		return domain.Post{}, err
	}
	defer tx.Rollback()

	if _, err := s.users.Get(ctx, in.AuthorID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			err = fmt.Errorf("%w: author %s does not exist", domain.ErrInvalid, in.AuthorID)
		}
		return domain.Post{}, err
	}
	p, err := s.posts.Insert(ctx, in)
	if err != nil {
		return domain.Post{}, err
	}
	return p, tx.Commit()
}

func (s *postService) Import(ctx context.Context, in iter.Seq2[domain.CreatePost, error]) (domain.PostImport, error) {
	var im postImport
	posts, err := s.posts.InsertEach(ctx, im.decoded(in))
	if err != nil {
		return domain.PostImport{}, err
	}
	for i, p := range posts {
		im.wrote(i, p, p.ID != domain.ID{})
	}
	return im.done(), nil
}

func (s *postService) ImportUnbatched(ctx context.Context, in iter.Seq2[domain.CreatePost, error]) (domain.PostImport, error) {
	var im postImport
	for _, err := range im.decoded(in) {
		if err != nil {
			return domain.PostImport{}, err
		}
	}

	// a transaction keeps a failure from leaving half the posts written, as
	// the batch's does
	tx, err := s.txer.Begin(&ctx, nil)
	if err != nil {
		return domain.PostImport{}, err
	}
	defer tx.Rollback()

	for i, q := range im.queued {
		p, err := s.posts.InsertIfAuthor(ctx, q.post)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return domain.PostImport{}, err
		}
		im.wrote(i, p, err == nil)
	}
	return im.done(), tx.Commit()
}

// postImport is an import in progress: what decoded, waiting to be written,
// and what came of it so far.
type postImport struct {
	out    domain.PostImport
	queued []queuedPost
}

type queuedPost struct {
	index int
	post  domain.CreatePost
}

// decoded yields the posts of in that decoded and queues them, and records the
// ones that broke a rule as failed. It ends at any other error, which it
// yields.
func (im *postImport) decoded(in iter.Seq2[domain.CreatePost, error]) iter.Seq2[domain.CreatePost, error] {
	return func(yield func(domain.CreatePost, error) bool) {
		i := 0
		for p, err := range in {
			if i == domain.MaxImport {
				yield(p, fmt.Errorf("%w: at most %d posts per import", domain.ErrInvalid, domain.MaxImport))
				return
			}
			if err != nil {
				es, ok := errors.AsType[ggen.Errors](err)
				if !ok {
					yield(p, fmt.Errorf("%w: %w", domain.ErrMalformed, err))
					return
				}
				msgs := make([]string, len(es))
				for j, e := range es {
					msgs[j] = e.Error()
				}
				im.out.Failed = append(im.out.Failed, domain.FailedPost{Index: i, Post: p, Errors: msgs})
			} else {
				im.queued = append(im.queued, queuedPost{i, p})
				if !yield(p, nil) {
					return
				}
			}
			i++
		}
	}
}

// wrote records the outcome of the i-th queued post: p when it was written,
// and its author missing when it was not.
func (im *postImport) wrote(i int, p domain.Post, ok bool) {
	if ok {
		im.out.Posts = append(im.out.Posts, p)
		return
	}
	q := im.queued[i]
	im.out.Failed = append(im.out.Failed, domain.FailedPost{
		Index:  q.index,
		Post:   q.post,
		Errors: []string{fmt.Sprintf("author_id: author %s does not exist", q.post.AuthorID)},
	})
}

// done orders the failed posts as they came and lists none as empty, not null.
func (im *postImport) done() domain.PostImport {
	out := im.out
	if out.Posts == nil {
		out.Posts = []domain.Post{}
	}
	if out.Failed == nil {
		out.Failed = []domain.FailedPost{}
	}
	slices.SortFunc(out.Failed, func(a, b domain.FailedPost) int { return cmp.Compare(a.Index, b.Index) })
	return out
}

func (s *postService) Delete(ctx context.Context, id domain.ID) error {
	return s.posts.Delete(ctx, id)
}
