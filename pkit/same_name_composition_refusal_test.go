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

func TestASameNamedAppCannotReplaceTheLiveCompositionOnOneDatabase(t *testing.T) {
	cfg := onOneDatabase(t)
	ledger := pkit.NewModule("ledger", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "ledger", Declared: []events.Declared{
			events.Declare[sharedDatabaseEvent]("ledger.recorded"),
		}}, nil
	})
	first, err := pkit.NewApp("collect").Use(doors, desk, ledger).
		Build(t.Context(), buildDeployment(cfg, app.All))
	if err != nil {
		t.Fatalf("build the live composition: %v", err)
	}
	defer first.Close()
	defer events.DeclareAll(nil)

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open the live application's database: %v", err)
	}
	defer conn.Close()
	actor := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	publishWrongShape := func() error {
		return db.Run(tenancy.WithTenant(t.Context(), actor), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, "ledger.recorded", map[string]any{"number": "wrong type"})
		})
	}
	if err := publishWrongShape(); err == nil || !strings.Contains(err.Error(), "the payload is not what the module declared") {
		t.Fatalf("the live composition did not install its event schema: %v", err)
	}

	// A rolling restart may repeat an application name, but it must not replace
	// the live composition's declared event contract while that app is serving.
	second, err := pkit.NewApp("collect").Use(doors, desk).
		Build(t.Context(), buildDeployment(cfg, app.All))
	if second != nil {
		defer second.Close()
	}
	if second != nil || err == nil {
		t.Errorf("a different composition with the live app name started: runtime=%v, err=%v", second, err)
	}
	if err := publishWrongShape(); err == nil || !strings.Contains(err.Error(), "the payload is not what the module declared") {
		t.Errorf("the live composition's event schema changed: %v", err)
	}
	admin := dbtest.Open(t, cfg.Database.MigrateURL)
	var count int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = 'ledger.recorded'").Scan(&count); err != nil {
		t.Fatalf("count the live app's outbox events: %v", err)
	}
	if count != 0 {
		t.Errorf("the malformed event committed %d outbox rows", count)
	}
}
