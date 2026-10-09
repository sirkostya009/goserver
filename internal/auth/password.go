//go:build !testhash

// Package auth holds what credentials are made of: password hashes, and the
// opaque tokens that sessions and api keys hand out.
package auth

import (
	"context"
	"runtime"
	"sync"

	"github.com/alexedwards/argon2id"
)

// hashParams is OWASP's minimum for argon2id. A hash records its own costs, so
// changing these only affects hashes made afterwards.
var hashParams = &argon2id.Params{Memory: 19 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}

// Every hash holds Memory KiB while it runs, so hashes wait for a slot: a flood
// of logins queues instead of exhausting memory.
var slots = make(chan struct{}, runtime.GOMAXPROCS(0))

// HashPassword returns password's hash in the PHC string format, which carries
// its own salt and costs.
func HashPassword(ctx context.Context, password string) (string, error) {
	if err := acquire(ctx); err != nil {
		return "", err
	}
	defer release()
	return argon2id.CreateHash(password, hashParams)
}

// VerifyPassword reports whether password matches encoded. An empty encoded,
// a user that does not exist or has no password, costs a hash all the same, so
// the time a login takes tells nothing about which part of it was wrong.
func VerifyPassword(ctx context.Context, encoded, password string) (bool, error) {
	known := encoded != ""
	if !known {
		encoded = dummy()
	}
	if err := acquire(ctx); err != nil {
		return false, err
	}
	defer release()
	ok, err := argon2id.ComparePasswordAndHash(password, encoded)
	return known && ok, err
}

// dummy is a hash of nothing anybody can type, made with the current costs.
var dummy = sync.OnceValue(func() string {
	h, err := HashPassword(context.Background(), "\x00")
	if err != nil {
		panic(err) // only a canceled context fails, and this one never is
	}
	return h
})

func acquire(ctx context.Context) error {
	select {
	case slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func release() { <-slots }
