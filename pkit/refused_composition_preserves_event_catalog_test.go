package pkit_test

import (
	"context"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/pkit"
)

type ledgerRecorded struct {
	Number int64 `json:"number"`
}

func TestRefusedBuildKeepsLiveEventSchema(t *testing.T) {
	firstConfig := onOneDatabase(t)
	ledger := pkit.NewModule("ledger", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "ledger", Declared: []events.Declared{
			events.Declare[ledgerRecorded]("ledger.recorded"),
		}}, nil
	})
	first, err := pkit.NewApp("first").Use(doors, desk, ledger).
		Build(t.Context(), buildDeployment(firstConfig, app.All))
	if err != nil {
		t.Fatalf("build the serving composition: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })
	t.Cleanup(func() { events.DeclareAll(nil) })

	conn, err := db.Open(t.Context(), firstConfig.Database.URL)
	if err != nil {
		t.Fatalf("open the serving composition's database: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	admin := dbtest.Open(t, firstConfig.Database.MigrateURL)
	countEvents := func() int {
		var count int
		if err := admin.QueryRowContext(t.Context(),
			"SELECT count(*) FROM platformkit_outbox WHERE name = 'ledger.recorded'").Scan(&count); err != nil {
			t.Fatalf("count committed ledger events: %v", err)
		}
		return count
	}
	actor := tenancy.Tenant{ID: uuid.New(), Slug: "first"}
	publishWrongPayload := func() error {
		return db.Run(tenancy.WithTenant(t.Context(), actor), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, "ledger.recorded", map[string]any{"number": "wrong type"})
		})
	}
	if err := publishWrongPayload(); err == nil || !strings.Contains(err.Error(), "the payload is not what the module declared") {
		t.Fatalf("the serving composition did not install its event schema: %v", err)
	}
	if got := countEvents(); got != 0 {
		t.Fatalf("a refused payload left %d outbox rows before the second build", got)
	}

	registrations := 0
	changing := pkit.NewModule("changing", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "changing", Routes: func(s httpx.Surfaces) {
			registrations++
			path := "/note"
			if registrations > 1 {
				path = "/later"
			}
			httpx.Register(s.App, huma.Operation{Method: "GET", OperationID: "changing.note", Path: path},
				httpx.SignedIn(), func(context.Context, *deskIn) (*deskOut, error) {
					return &deskOut{Note: "unchanged"}, nil
				})
		}}, nil
	})
	t.Run("other database", func(t *testing.T) {
		secondConfig := onOneDatabase(t)
		second, err := pkit.NewApp("second").Use(doors, changing).
			Build(t.Context(), buildDeployment(secondConfig, app.All))
		if second != nil {
			defer second.Close()
			t.Error("a composition with changing routes returned a runtime")
		}
		if err == nil || !strings.Contains(err.Error(), "/note") || !strings.Contains(err.Error(), "/later") {
			t.Fatalf("a composition with changing routes was not refused with its two paths: %v", err)
		}
		if registrations != 2 {
			t.Fatalf("the refused composition registered routes %d times, want two dry registrations", registrations)
		}
		if got := tablesIn(t, secondConfig.Database.MigrateURL); got != 0 {
			t.Errorf("a refused composition migrated %d tables", got)
		}
	})
	if err := publishWrongPayload(); err == nil || !strings.Contains(err.Error(), "the payload is not what the module declared") {
		t.Errorf("the serving composition's event schema changed after another build was refused: %v", err)
	}
	if got := countEvents(); got != 0 {
		t.Errorf("a refused build let a mis-shaped event commit %d outbox rows", got)
	}
}
