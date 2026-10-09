// Package errs gives errors the stack of where they were made, which the
// standard library's never carry. Expected outcomes, the domain sentinels, need
// none: this is for failures someone will have to trace back.
package errs

import (
	"errors"
	"fmt"
	"runtime"
)

const depth = 32

// Error is an error and the stack it was made on.
type Error struct {
	err error
	n   int
	pcs [depth]uintptr // inline, so making one costs a single allocation
}

// New is errors.New with the caller's stack.
func New(msg string) error {
	return capture(errors.New(msg))
}

// Errorf is fmt.Errorf with the caller's stack, unless an error it wraps
// already carries one, which is closer to the cause.
func Errorf(format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	if HasStack(err) {
		return err
	}
	return capture(err)
}

// Wrap attaches the caller's stack to err. It returns nil for nil, and err as
// is when it already carries a stack.
func Wrap(err error) error {
	if err == nil || HasStack(err) {
		return err
	}
	return capture(err)
}

// HasStack reports whether any error in err's chain carries a stack.
func HasStack(err error) bool {
	_, ok := errors.AsType[*Error](err)
	return ok
}

func capture(err error) *Error {
	e := &Error{err: err}
	// skip runtime.Callers, capture and the exported func that called it
	e.n = runtime.Callers(3, e.pcs[:])
	return e
}

func (e *Error) Error() string { return e.err.Error() }

func (e *Error) Unwrap() error { return e.err }

// StackTrace is the program counters of the stack, innermost call first. The
// name and shape are the convention Sentry, among others, looks for.
func (e *Error) StackTrace() []uintptr { return e.pcs[:e.n] }

// Frames resolves the stack, innermost call first.
func (e *Error) Frames() *runtime.Frames { return runtime.CallersFrames(e.StackTrace()) }
