package migrations_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sirkostya009/barm"

	"goserver/internal/migrations"
	"goserver/internal/pgtest"
	"goserver/internal/repository"
)

func TestApply_EveryCategory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := repository.Open(ctx, pgtest.NewDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for schema, c := range map[string]migrations.Category{
		migrations.SharedSchema: migrations.Shared,
		"tenant_a":              migrations.Tenant,
		"tenant_b":              migrations.Tenant,
	} {
		var behind *migrations.BehindError
		if err := migrations.Check(ctx, db, c, schema); !errors.As(err, &behind) || behind.Have != 0 {
			t.Fatalf("%s fresh check: %v", schema, err)
		}
		n, err := migrations.Apply(ctx, db, c, schema)
		if err != nil || n != migrations.Latest(c) {
			t.Fatalf("%s apply: %d %v", schema, n, err)
		}
		if n, err := migrations.Apply(ctx, db, c, schema); err != nil || n != 0 {
			t.Errorf("%s second apply is not a no-op: %d %v", schema, n, err)
		}
		if err := migrations.Check(ctx, db, c, schema); err != nil {
			t.Errorf("%s check after apply: %v", schema, err)
		}
	}

	// two tenants got a users table each, nothing landed in public
	tables := func() *barm.SelectQuery[int] {
		return db.Select[int]().Table("information_schema.tables").ColumnExpr("count(*)")
	}
	n, err := tables().Where("table_name = 'users'").One(ctx)
	if err != nil || n != 2 {
		t.Errorf("users tables: %d %v", n, err)
	}
	n, err = tables().Where("table_schema = 'public'").One(ctx)
	if err != nil || n != 0 {
		t.Errorf("tables leaked into public: %d %v", n, err)
	}
}

func TestApply_Concurrent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := repository.Open(ctx, pgtest.NewDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var wg sync.WaitGroup
	total := make(chan int, 8)
	for range 8 {
		wg.Go(func() {
			n, err := migrations.Apply(ctx, db, migrations.Tenant, "tenant_a")
			if err != nil {
				t.Error(err)
			}
			total <- n
		})
	}
	wg.Wait()
	close(total)
	sum := 0
	for n := range total {
		sum += n
	}
	if sum != migrations.Latest(migrations.Tenant) {
		t.Errorf("applied %d migrations across runners, want %d", sum, migrations.Latest(migrations.Tenant))
	}
}
