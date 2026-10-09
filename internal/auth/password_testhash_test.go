//go:build testhash

package auth

import "testing"

func TestTesthash_RoundTrip(t *testing.T) {
	t.Parallel()
	h, _ := HashPassword(t.Context(), "hunter22")
	for pw, want := range map[string]bool{"hunter22": true, "hunter2": false, "": false} {
		if ok, err := VerifyPassword(t.Context(), h, pw); err != nil || ok != want {
			t.Errorf("%q: %t %v, want %t", pw, ok, err, want)
		}
	}
	// no hash, or an argon2id one from a real build, never matches
	for _, encoded := range []string{"", "$argon2id$v=19$m=64,t=1,p=1$c2FsdA$a2V5"} {
		if ok, _ := VerifyPassword(t.Context(), encoded, "hunter22"); ok {
			t.Errorf("%q matched", encoded)
		}
	}
}
