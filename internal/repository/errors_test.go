package repository

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"goserver/domain"
	"goserver/internal/errs"
)

func TestMapErr_Unexpected_GainsStack(t *testing.T) {
	t.Parallel()
	cause := errors.New("connection reset")
	err := mapErr(cause)
	if !errors.Is(err, cause) || !errs.HasStack(err) {
		t.Errorf("%v: want the cause, with a stack", err)
	}
}

// expected outcomes are answers, not failures: they get no stack
func TestMapErr_Sentinels_NoStack(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   error
		want error
	}{
		{sql.ErrNoRows, domain.ErrNotFound},
		{&pgconn.PgError{Code: uniqueViolation}, domain.ErrConflict},
		{&pgconn.PgError{Code: undefinedTable}, domain.ErrUnknownTenant},
	} {
		err := mapErr(tc.in)
		if !errors.Is(err, tc.want) || errs.HasStack(err) {
			t.Errorf("%v: got %v, stack %t", tc.in, err, errs.HasStack(err))
		}
	}
}
