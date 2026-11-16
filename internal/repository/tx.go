package repository

import (
	"context"
	"database/sql"

	"github.com/sirkostya009/barm"

	"goserver/domain"
)

// Tx is a running transaction, as the layer that opened it sees it: barm's own
// *barm.Tx satisfies it. It is an interface only so a service can be tested
// without a database — at runtime it is always the barm transaction.
type Tx interface {
	Commit() error
	// Rollback undoes the transaction, and does nothing once it has been
	// committed, so `defer tx.Rollback()` is the whole cleanup.
	Rollback() error
}

// Txer opens transactions. A service takes one when it has writes that must
// land together.
type Txer interface {
	// Begin starts a transaction and points ctx at it, so every repository call
	// made with that ctx runs inside it. When ctx already carries one, the new
	// transaction nests as a savepoint of it.
	//
	// ctx is replaced rather than returned so a caller cannot hold on to the
	// outer context by accident and write outside the transaction it just
	// opened.
	Begin(ctx *context.Context, opts *sql.TxOptions) (Tx, error)
}

type txer struct{ db *barm.DB }

func (t txer) Begin(ctx *context.Context, opts *sql.TxOptions) (Tx, error) {
	var (
		tx  *barm.Tx
		err error
	)
	if outer, ok := txFrom(*ctx); ok {
		tx, err = outer.BeginTx(*ctx, opts)
	} else {
		tx, err = t.db.BeginTx(*ctx, opts)
	}
	if err != nil {
		return nil, mapErr(err)
	}
	//nolint:fatcontext // deliberate: Begin replaces the caller's ctx so calls made with it run inside the tx
	*ctx = context.WithValue(*ctx, txKey{}, tx)
	return tx, nil
}

type txKey struct{}

func txFrom(ctx context.Context) (*barm.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(*barm.Tx)
	return tx, ok
}

// base is what every repository is: a pool, and the handle a call runs on.
type base struct{ db *barm.DB }

// handle is what this call runs on — the transaction in ctx when there is one,
// otherwise the pool. Handle wraps it because barm's builders are generic
// methods, which an interface cannot carry.
func (b base) handle(ctx context.Context) barm.Handle {
	if tx, ok := txFrom(ctx); ok {
		return barm.Handle{IDB: tx}
	}
	return barm.Handle{IDB: b.db}
}

// tenantSchema is the request tenant's: Select[T]().Schema(tenantSchema(ctx)).
func tenantSchema(ctx context.Context) string {
	return domain.MustTenant(ctx).Schema
}
