// Package pgtest gives tests a postgres to run against.
package pgtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirkostya009/barm"
	"github.com/sirkostya009/barm/pgxdriver"

	"goserver/internal/migrations"
)

const defaultAdminURL = "postgres://postgres@localhost:5432/postgres?sslmode=disable"

func adminURL() string {
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		return u
	}
	return defaultAdminURL
}

// NewDatabase creates an empty database, dropped when the test ends, and
// returns its url. It is for tests that need a database to themselves, such as
// the ones that migrate it. The test is skipped when no server answers.
func NewDatabase(t *testing.T) string {
	t.Helper()
	admin, err := dial(adminURL())
	if err != nil {
		t.Skipf("no postgres at TEST_DATABASE_URL: %v", err)
	}

	name, err := create(admin)
	if err != nil {
		admin.Close(context.Background())
		t.Fatal(err)
	}
	t.Cleanup(func() {
		drop(admin, name)
		admin.Close(context.Background())
	})
	return nameToURL(t, name)
}

// Run is what a package's TestMain calls. It creates one database for the whole
// test binary, applies the shared migrations to it, and drops it once the tests
// are done — so DB hands out a transaction rather than a database each.
func Run(m *testing.M) int {
	unavailable = setup()
	defer teardown()
	return m.Run()
}

var (
	unavailable error
	admin       *pgx.Conn
	dbName      string
	pool        barm.Pool
)

func setup() error {
	var err error
	if admin, err = dial(adminURL()); err != nil {
		return fmt.Errorf("no postgres at TEST_DATABASE_URL: %w", err)
	}
	if dbName, err = create(admin); err != nil {
		return err
	}

	u, err := url.Parse(adminURL())
	if err != nil {
		return err
	}
	u.Path = "/" + dbName
	ctx := context.Background()
	p, err := pgxpool.New(ctx, u.String())
	if err != nil {
		return err
	}
	pool = pgxdriver.Pool(p)
	// the registry every tenant is looked up in outlives the tests; everything
	// each test writes is rolled back around it
	_, err = migrations.Apply(ctx, barm.New(pool), migrations.Shared, migrations.SharedSchema)
	return err
}

func teardown() {
	if admin == nil {
		return
	}
	if pool != nil {
		pool.Close()
	}
	if dbName != "" {
		drop(admin, dbName)
	}
	admin.Close(context.Background())
}

// DB returns a handle on the shared database whose every write is undone when
// the test ends: the handle is one transaction, rolled back on close. Tests
// stay isolated without paying for a database each.
func DB(t *testing.T) *barm.DB {
	t.Helper()
	if unavailable != nil {
		t.Skip(unavailable)
	}
	tx, err := pool.Begin(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &txPool{DriverTx: tx}
	t.Cleanup(func() { p.Close() })
	return barm.New(p)
}

// txPool is a barm.Pool on one transaction: a connection is the transaction
// itself and a transaction is a savepoint in it. Like the one connection under
// it, it serves one goroutine at a time.
type txPool struct {
	barm.DriverTx

	seq int
}

func (*txPool) Dialect() barm.Dialect { return pool.Dialect() }

func (*txPool) NativeJSON() bool {
	n, ok := pool.(barm.NativeJSON)
	return ok && n.NativeJSON()
}

func (*txPool) NativeArrays() bool {
	n, ok := pool.(barm.NativeArrays)
	return ok && n.NativeArrays()
}

func (*txPool) Ping(ctx context.Context) error { return pool.Ping(ctx) }

func (*txPool) Underlying() any { return pool.Underlying() }

func (p *txPool) Acquire(context.Context) (barm.DriverConn, error) { return conn{p}, nil }

func (p *txPool) Begin(ctx context.Context, _ *sql.TxOptions) (barm.DriverTx, error) {
	p.seq++
	sp := savepoint{p.DriverTx, "pgtest_sp_" + strconv.Itoa(p.seq)}
	if _, err := p.Exec(ctx, "SAVEPOINT "+sp.name, nil); err != nil {
		return nil, err
	}
	return sp, nil
}

func (p *txPool) Close() error { return p.Rollback(context.Background()) }

type conn struct{ *txPool }

func (conn) Release() error { return nil }

type savepoint struct {
	barm.DriverTx

	name string
}

func (s savepoint) Commit(ctx context.Context) error {
	_, err := s.Exec(ctx, "RELEASE SAVEPOINT "+s.name, nil)
	return err
}

func (s savepoint) Rollback(ctx context.Context) error {
	if _, err := s.Exec(ctx, "ROLLBACK TO SAVEPOINT "+s.name, nil); err != nil {
		return err
	}
	return s.Commit(ctx)
}

func dial(u string) (*pgx.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return pgx.Connect(ctx, u)
}

func create(admin *pgx.Conn) (string, error) {
	var suffix [6]byte
	rand.Read(suffix[:])
	name := "goserver_test_" + hex.EncodeToString(suffix[:])
	if _, err := admin.Exec(context.Background(), `create database `+name); err != nil {
		return "", errors.New("create database: " + err.Error())
	}
	return name, nil
}

func drop(admin *pgx.Conn, name string) {
	_, _ = admin.Exec(context.Background(), `drop database if exists `+name+` with (force)`)
}

func nameToURL(t *testing.T, name string) string {
	t.Helper()
	u, err := url.Parse(adminURL())
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}
