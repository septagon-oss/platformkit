package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/internal/exampletest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func ExampleRun() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err := dbtest.WithSchema(ctx, "tenant transaction", func(ctx context.Context, _ *sql.DB, conn *db.Conn) error {
		tenant := tenancy.Tenant{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Slug: "example"}
		var id uuid.UUID
		var system bool
		var slug string
		err := db.Run(tenancy.WithTenant(ctx, tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			slug = db.TenantOf(tx).Slug
			return tx.DB().Raw("SELECT platformkit_current_tenant_id(), platformkit_is_system()").Row().Scan(&id, &system)
		})
		if err == nil {
			fmt.Println(slug, id == tenant.ID, system)
		}
		return err
	})
	if err != nil {
		panic(err)
	}
	// Output: example true false
}

func ExampleRunSystem() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err := dbtest.WithSchema(ctx, "system transaction", func(ctx context.Context, _ *sql.DB, conn *db.Conn) error {
		// This token is fixture-only; a module receives its capability from composition.
		token := dbtest.SystemToken()
		var system bool
		err := db.RunSystem(ctx, conn, token, func(_ context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Raw("SELECT platformkit_is_system()").Row().Scan(&system)
		})
		if err == nil {
			fmt.Println("system:", system)
		}
		return err
	})
	if err != nil {
		panic(err)
	}
	// Output: system: true
}

func TestDocumentedExamplesExecute(t *testing.T) {
	exampletest.Execute(t, "ExampleRun", "ExampleRunSystem")
}
