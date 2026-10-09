//go:build !testhash

package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/alexedwards/argon2id"
)

func TestMain(m *testing.M) {
	hashParams = &argon2id.Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	m.Run()
}

func TestPassword_RoundTrip(t *testing.T) {
	t.Parallel()
	h, err := HashPassword(t.Context(), "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Errorf("encoding %q", h)
	}
	for pw, want := range map[string]bool{"correct horse": true, "correct horse ": false, "": false} {
		if ok, err := VerifyPassword(t.Context(), h, pw); err != nil || ok != want {
			t.Errorf("%q: %t %v, want %t", pw, ok, err, want)
		}
	}
}

func TestPassword_SaltedPerHash(t *testing.T) {
	t.Parallel()
	a, _ := HashPassword(t.Context(), "pw")
	b, _ := HashPassword(t.Context(), "pw")
	if a == b {
		t.Error("two hashes of one password are equal")
	}
}

// the hash records its costs, so raising them later keeps old hashes working
func TestPassword_VerifiesWithItsOwnParams(t *testing.T) {
	t.Parallel()
	const old = "$argon2id$v=19$m=32,t=1,p=1$c2FsdHNhbHRzYWx0c2FsdA$"
	h, _ := HashPassword(t.Context(), "pw")
	key := h[strings.LastIndexByte(h, '$')+1:]
	salt := strings.Split(h, "$")[4]
	// same salt and key, recomputed under other costs, must not match
	if ok, _ := VerifyPassword(t.Context(), "$argon2id$v=19$m=32,t=1,p=1$"+salt+"$"+key, "pw"); ok {
		t.Error("costs are not read from the hash")
	}
	if _, err := VerifyPassword(t.Context(), old+"AAAA", "pw"); err != nil {
		t.Errorf("old costs: %v", err)
	}
}

func TestPassword_NoHash_NeverMatches(t *testing.T) {
	t.Parallel()
	for _, pw := range []string{"", "\x00", "anything"} {
		if ok, err := VerifyPassword(t.Context(), "", pw); ok || err != nil {
			t.Errorf("%q: %t %v", pw, ok, err)
		}
	}
}

func TestPassword_Malformed(t *testing.T) {
	t.Parallel()
	for _, h := range []string{"plain", "$argon2i$v=19$m=64,t=1,p=1$c2FsdA$a2V5", "$argon2id$v=18$m=64,t=1,p=1$c2FsdA$a2V5", "$argon2id$v=19$m=x$c2FsdA$a2V5", "$argon2id$v=19$m=64,t=1,p=1$!!$a2V5"} {
		if _, err := VerifyPassword(t.Context(), h, "pw"); err == nil {
			t.Errorf("%q accepted", h)
		}
	}
}

func TestPassword_Canceled_Waits(t *testing.T) {
	t.Parallel()
	for range cap(slots) {
		slots <- struct{}{}
	}
	defer func() {
		for range cap(slots) {
			<-slots
		}
	}()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := HashPassword(ctx, "pw"); err == nil {
		t.Error("hashed with every slot taken and the context gone")
	}
}
