package pkit_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestTwoDatabasesKeepTheirEventSchemas(t *testing.T) {
	firstDB := onOneDatabase(t)
	ledger := pkit.NewModule("ledger", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "ledger", Declared: []events.Declared{
			events.Declare[sharedDatabaseEvent]("ledger.recorded"),
		}}, nil
	})
	first, err := pkit.NewApp("collect").Use(doors, desk, ledger).
		Build(t.Context(), buildDeployment(firstDB, app.All))
	if err != nil {
		t.Fatalf("build the first application: %v", err)
	}
	defer first.Close()
	defer events.DeclareAll(nil)

	conn, err := db.Open(t.Context(), firstDB.Database.URL)
	if err != nil {
		t.Fatalf("open the first database: %v", err)
	}
	defer conn.Close()
	actor := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	publishWrongShape := func() error {
		return db.Run(tenancy.WithTenant(t.Context(), actor), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, "ledger.recorded", map[string]any{"number": "wrong type"})
		})
	}
	if err := publishWrongShape(); err == nil || !strings.Contains(err.Error(), "the payload is not what the module declared") {
		t.Fatalf("the first application did not install its event schema: %v", err)
	}

	t.Run("another_database", func(t *testing.T) {
		otherDB := onOneDatabase(t)
		if otherDB.Database.URL == firstDB.Database.URL {
			t.Fatal("the second application did not receive another database")
		}
		second, err := pkit.NewApp("wishlist").Use(doors, desk).
			Build(t.Context(), buildDeployment(otherDB, app.All))
		if err != nil {
			t.Fatalf("build the application on another database: %v", err)
		}
		defer second.Close()
	})

	if err := publishWrongShape(); err == nil || !strings.Contains(err.Error(), "the payload is not what the module declared") {
		t.Errorf("the first application's event schema changed after another database booted: %v", err)
	}
	admin := dbtest.Open(t, firstDB.Database.MigrateURL)
	var count int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = 'ledger.recorded'").Scan(&count); err != nil {
		t.Fatalf("count the first application's outbox events: %v", err)
	}
	if count != 0 {
		t.Errorf("the malformed event committed %d outbox rows", count)
	}
}
