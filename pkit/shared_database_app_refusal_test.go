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

type sharedDatabaseEvent struct {
	Number int64 `json:"number"`
}

func TestASecondServerCannotReplaceTheFirstAppsEventSchemaOnSharedDatabase(t *testing.T) {
	cfg := onOneDatabase(t)
	ledger := pkit.NewModule("ledger", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "ledger", Declared: []events.Declared{
			events.Declare[sharedDatabaseEvent]("ledger.recorded"),
		}}, nil
	})
	first, err := pkit.NewServer().Deploy(buildDeployment(cfg, app.All)).
		Host(pkit.NewApp("collect").Use(doors, desk, ledger), pkit.Tenant("Acme", "acme.test")).
		Build(t.Context())
	if err != nil {
		t.Fatalf("build the first app: %v", err)
	}
	defer first.Close()
	defer events.DeclareAll(nil)

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open the first app's database: %v", err)
	}
	defer conn.Close()
	actor := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	publishWrongShape := func() error {
		return db.Run(tenancy.WithTenant(t.Context(), actor), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, "ledger.recorded", map[string]any{"number": "wrong type"})
		})
	}
	if err := publishWrongShape(); err == nil || !strings.Contains(err.Error(), "the payload is not what the module declared") {
		t.Fatalf("the first app did not install its event schema: %v", err)
	}

	second, err := pkit.NewServer().Deploy(buildDeployment(cfg, app.All)).
		Host(pkit.NewApp("wishlist").Use(doors, desk), pkit.Tenant("Globex", "globex.test")).
		Build(t.Context())
	if second != nil {
		defer second.Close()
	}
	if second != nil || err == nil || !strings.Contains(err.Error(), "T-0231") {
		t.Errorf("a second app started on the first app's database: runtime=%v, err=%v", second, err)
	}
	if err := publishWrongShape(); err == nil || !strings.Contains(err.Error(), "the payload is not what the module declared") {
		t.Errorf("the first app's event schema changed after the second app attempted to start: %v", err)
	}
	admin := dbtest.Open(t, cfg.Database.MigrateURL)
	var count int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = 'ledger.recorded'").Scan(&count); err != nil {
		t.Fatalf("count the first app's outbox events: %v", err)
	}
	if count != 0 {
		t.Errorf("the refused malformed event committed %d outbox rows", count)
	}
}
