package errs

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func topFunction(t *testing.T, err error) string {
	t.Helper()
	e, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("%v carries no stack", err)
	}
	frame, _ := e.Frames().Next()
	return frame.Function
}

func TestNew_Stack_StartsAtCaller(t *testing.T) {
	t.Parallel()
	if fn := topFunction(t, New("boom")); !strings.HasSuffix(fn, "TestNew_Stack_StartsAtCaller") {
		t.Errorf("top frame %s", fn)
	}
}

func origin() error { return New("deep") }

func TestErrorf_WrapsStacked_KeepsOrigin(t *testing.T) {
	t.Parallel()
	err := Errorf("outer: %w", origin())
	if _, ok := err.(*Error); ok { //nolint:errorlint // the outermost error is the point
		t.Error("captured a second stack over one already in the chain")
	}
	if fn := topFunction(t, err); !strings.HasSuffix(fn, ".origin") {
		t.Errorf("top frame %s, want origin", fn)
	}
	if err.Error() != "outer: deep" {
		t.Errorf("message %q", err)
	}
}

func TestWrap_Plain_GainsStack(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("driver said no")
	err := Wrap(sentinel)
	if !errors.Is(err, sentinel) || err.Error() != sentinel.Error() {
		t.Errorf("wrap changed the error: %v", err)
	}
	if fn := topFunction(t, err); !strings.HasSuffix(fn, "TestWrap_Plain_GainsStack") {
		t.Errorf("top frame %s", fn)
	}
}

func TestWrap_NilAndStacked_Unchanged(t *testing.T) {
	t.Parallel()
	if Wrap(nil) != nil {
		t.Error("nil wrapped into an error")
	}
	stacked := fmt.Errorf("ctx: %w", origin())
	if Wrap(stacked) != stacked { //nolint:errorlint // identity is the point
		t.Error("stacked error wrapped again")
	}
}
