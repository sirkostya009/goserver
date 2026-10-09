package repository

import (
	"context"
	"database/sql"

	"github.com/sirkostya009/barm"

	"goserver/internal/cache"
)

// txState is what the cache knows of a transaction: which tables it wrote, and
// the ticket its reads fill under. Savepoints share their transaction's.
type txState struct {
	ticket uint64
	dirty  table
}

type txStateKey struct{}

func txStateFrom(ctx context.Context) (*txState, bool) {
	st, ok := ctx.Value(txStateKey{}).(*txState)
	return st, ok
}

// cachedTxer opens transactions whose reads and writes the cached repositories
// track.
type cachedTxer struct {
	txer  Txer
	clock *cache.Clock
}

func (t cachedTxer) Begin(ctx *context.Context, opts *sql.TxOptions) (Tx, error) {
	if _, ok := txStateFrom(*ctx); ok {
		return t.txer.Begin(ctx, opts)
	}
	// taken before BEGIN, so it is no later than any snapshot the transaction reads
	st := &txState{ticket: t.clock.Ticket()}
	tx, err := t.txer.Begin(ctx, opts)
	if err != nil {
		return nil, err
	}
	//nolint:fatcontext // deliberate: Begin replaces the caller's ctx so calls made with it run inside the tx
	*ctx = context.WithValue(*ctx, txStateKey{}, st)
	return tx, nil
}

// wrote marks t written by ctx's transaction and runs invalidate once the write
// is durable: on commit, or now outside a transaction. A savepoint rolled back
// drops its invalidations with its writes.
func wrote(ctx context.Context, t table, invalidate func()) {
	st, ok := txStateFrom(ctx)
	if !ok {
		if invalidate != nil {
			invalidate()
		}
		return
	}
	st.dirty |= t
	if invalidate != nil {
		tx, _ := txFrom(ctx)
		tx.WithTxHook(barm.TxHook{AfterCommit: func(context.Context, *barm.TxEvent) { invalidate() }})
	}
}
