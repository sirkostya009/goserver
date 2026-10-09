package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirkostya009/barm"
	"github.com/sirkostya009/barm/pgxdriver"

	"goserver/domain"
	"goserver/internal/errs"
	"goserver/internal/migrations"
)

// Open connects to postgres without looking at what is in it.
func Open(ctx context.Context, url string, opts ...barm.Option) (*barm.DB, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	return barm.New(pgxdriver.Pool(pool), opts...), nil
}

// OpenChecked is Open for the backend: it fails with a *migrations.BehindError
// when the shared schema is behind, rather than migrate it.
func OpenChecked(ctx context.Context, url string, opts ...barm.Option) (*barm.DB, error) {
	db, err := Open(ctx, url, opts...)
	if err != nil {
		return nil, err
	}
	if err := migrations.Check(ctx, db, migrations.Shared, migrations.SharedSchema); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

const (
	uniqueViolation     = "23505"
	foreignKeyViolation = "23503"
	invalidSchemaName   = "3F000"
	undefinedTable      = "42P01"
)

// mapErr translates driver errors into domain sentinels so upper layers never
// see sql or pg types, and gives any other error a stack.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgErr.Code {
		case uniqueViolation:
			return domain.ErrConflict
		case foreignKeyViolation:
			return domain.ErrInvalid
		// tenant schemas are derived from the id and never looked up, so a tenant
		// that does not exist, or is not migrated yet, shows up as a missing
		// relation. postgres reports a missing schema the same way on a query.
		case undefinedTable, invalidSchemaName:
			return fmt.Errorf("%w: %w", domain.ErrUnknownTenant, err)
		}
	}
	return errs.Wrap(err)
}

func deleted(res sql.Result, err error) error {
	if err != nil {
		return mapErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
