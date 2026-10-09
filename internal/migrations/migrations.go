// Package migrations holds the schema of everything the backend reads and the
// runner that applies it. The backend never applies anything: it only calls
// Check, and cmd/migrate is what calls Apply.
//
// Migration files name no schema. Each category is applied to a schema of its
// own, the shared one or a tenant's, by pointing search_path at it.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strconv"
	"strings"

	"github.com/sirkostya009/barm"
)

// Category is one kind of schema, with a migration history of its own.
type Category string

const (
	Shared Category = "shared"
	Tenant Category = "tenant"
)

// SharedSchema is the schema holding what every tenant shares.
const SharedSchema = "shared"

//go:embed shared/*.sql tenant/*.sql
var files embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

var byCategory = map[Category][]migration{
	Shared: load(Shared),
	Tenant: load(Tenant),
}

// load reads NNNN_name.sql files. embed.FS lists them sorted, which is version
// order as long as the numbers are zero-padded.
func load(c Category) []migration {
	entries, err := files.ReadDir(string(c))
	if err != nil {
		panic(err)
	}
	out := make([]migration, 0, len(entries))
	for _, e := range entries {
		num, name, ok := strings.Cut(strings.TrimSuffix(e.Name(), ".sql"), "_")
		version, err := strconv.Atoi(num)
		if !ok || err != nil || version != len(out)+1 {
			panic(fmt.Sprintf("migrations: %s/%s: want NNNN_name.sql numbered without gaps", c, e.Name()))
		}
		body, err := files.ReadFile(string(c) + "/" + e.Name())
		if err != nil {
			panic(err)
		}
		out = append(out, migration{version, name, string(body)})
	}
	return out
}

// Latest is the version a fully migrated schema of this category is at.
func Latest(c Category) int {
	return len(byCategory[c])
}

// BehindError says a schema has not been migrated far enough to be used.
type BehindError struct {
	Schema     string
	Have, Want int
}

func (e *BehindError) Error() string {
	return fmt.Sprintf("schema %s is at migration %d, want %d: run cmd/migrate", e.Schema, e.Have, e.Want)
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Version reports the last migration applied to schema, 0 for one that has
// never been migrated or does not exist.
func Version(ctx context.Context, db barm.IDB, schema string) (int, error) {
	table := quoteIdent(schema) + ".schema_migrations"
	var tracked bool
	if err := scanRow(ctx, db, &tracked, `select to_regclass($1) is not null`, table); err != nil || !tracked {
		return 0, err
	}
	var version int
	err := scanRow(ctx, db, &version, `select coalesce(max(version), 0) from `+table)
	return version, err
}

func scanRow(ctx context.Context, db barm.IDB, dest any, query string, args ...any) error {
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := rows.Scan(dest); err != nil {
		return err
	}
	return rows.Close()
}

// Check fails with a *BehindError when schema is missing migrations. A schema
// ahead of this binary is fine: that is what a rolling deploy looks like.
func Check(ctx context.Context, db barm.IDB, c Category, schema string) error {
	have, err := Version(ctx, db, schema)
	if err != nil {
		return err
	}
	if want := Latest(c); have < want {
		return &BehindError{Schema: schema, Have: have, Want: want}
	}
	return nil
}

// Apply runs every migration schema is missing, each in its own transaction,
// creating the schema first if need be. It returns how many it applied.
func Apply(ctx context.Context, db barm.IDB, c Category, schema string) (int, error) {
	for n := 0; ; n++ {
		applied, err := step(ctx, db, c, schema)
		if err != nil || !applied {
			return n, err
		}
	}
}

// step applies the next pending migration, if any. The advisory lock makes two
// concurrent runs take turns, and the version is read under it, so neither
// applies what the other just did.
func step(ctx context.Context, db barm.IDB, c Category, schema string) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	ident := quoteIdent(schema)
	for _, stmt := range []string{
		`select pg_advisory_xact_lock(hashtext('goserver.migrations.' || $1))`,
		`create schema if not exists ` + ident,
		`set local search_path = ` + ident,
		`create table if not exists schema_migrations (
			version    integer     primary key,
			name       text        not null,
			applied_at timestamptz not null default now()
		)`,
	} {
		args := []any{}
		if strings.Contains(stmt, "$1") {
			args = append(args, schema)
		}
		if _, err := tx.Exec(ctx, stmt, args...); err != nil {
			return false, err
		}
	}

	have, err := Version(ctx, tx, schema)
	if err != nil {
		return false, err
	}
	all := byCategory[c]
	if have >= len(all) {
		return false, nil
	}
	m := all[have]

	for stmt := range strings.SplitSeq(m.sql, ";") {
		if stmt = strings.TrimSpace(stmt); stmt == "" {
			continue
		}
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return false, fmt.Errorf("%s migration %04d_%s: %w", c, m.version, m.name, err)
		}
	}
	if _, err := tx.Exec(ctx, `insert into schema_migrations (version, name) values ($1, $2)`, m.version, m.name); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
