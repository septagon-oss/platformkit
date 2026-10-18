package pkit_test

// The declared shape of an event is a promise its subscribers read, and kit/events
// checks every outbox INSERT against it. The catalog that answers is keyed by the
// app whose tenant the row belongs to, so one app holds one shape per event name
// while any of its compositions is live: a second boot of the same app may not
// spell a name the standing one already chose another way, and two different apps
// may, because each is measured against its own entry and its tenants' own row says
// which entry that is (kit/events/catalog.go). The refusal belongs to the
// composition rather than to the database, so it is answered in kit/app's
// constructor, with the deployment the second application names still undialed.

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

type postedNumber struct {
	Number int64 `json:"number"`
}

// postedText is the same event name with a payload that is not the same document.
type postedText struct {
	Number string `json:"number"`
}

// postedModule is a ledger whose one event is spelled with the payload T names.
func postedModule[T any](name string) *pkit.Module {
	return pkit.NewModule(name, func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: name, Declared: []events.Declared{
			events.Declare[T]("ledger.posted"),
		}}, nil
	})
}

func TestOneEventNameHasOneShapeInOneProcess(t *testing.T) {
	first := onOneDatabase(t)
	live, err := pkit.NewApp("collect").Use(doors, desk, postedModule[postedNumber]("ledger")).
		Build(t.Context(), buildDeployment(first, app.All))
	if err != nil {
		t.Fatalf("build the live application: %v", err)
	}
	defer live.Close()
	defer events.DeclareAll(nil)

	conn, err := db.Open(t.Context(), first.Database.URL)
	if err != nil {
		t.Fatalf("open the live application's database: %v", err)
	}
	defer conn.Close()
	actor := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	publishNumber := func() error {
		return db.Run(tenancy.WithTenant(t.Context(), actor), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, "ledger.posted", map[string]any{"number": "wrong type"})
		})
	}
	if err := publishNumber(); err == nil || !strings.Contains(err.Error(), "the payload is not what the module declared") {
		t.Fatalf("the live application did not install its event schema: %v", err)
	}
	admin := dbtest.Open(t, first.Database.MigrateURL)

	// The second application is on a database of its own, so nothing about the
	// deployment refuses it: the event name is the whole collision, and it is
	// answered with both stores undialed. The subtest is what gives it a schema of
	// its own to be pointed at.
	t.Run("live", func(t *testing.T) {
		other := onOneDatabase(t)
		second, err := pkit.NewApp("collect").Use(doors, desk, postedModule[postedText]("ledger")).
			Build(t.Context(), buildDeployment(other, app.All))
		if second != nil {
			defer second.Close()
		}
		if second != nil || err == nil {
			t.Fatalf("a second application spelled a standing event name another way and started: runtime=%v, err=%v", second, err)
		}
		says(t, err, "ledger.posted")
		if got := tablesIn(t, other.Database.MigrateURL); got != 0 {
			t.Errorf("the refused composition migrated %d tables on its own database", got)
		}
		// The refusal installed nothing, so the shape the live application answers
		// under is the one it declared, and it is still the one its outbox checks.
		if err := publishNumber(); err == nil || !strings.Contains(err.Error(), "the payload is not what the module declared") {
			t.Errorf("the live application's event schema changed beside a refused boot: %v", err)
		}
		var count int
		if err := admin.QueryRowContext(t.Context(),
			"SELECT count(*) FROM platformkit_outbox WHERE name = 'ledger.posted'").Scan(&count); err != nil {
			t.Fatalf("count the live application's outbox events: %v", err)
		}
		if count != 0 {
			t.Errorf("the malformed event committed %d outbox rows", count)
		}
	})

	// And the refused application met nothing on the way to that answer: once the
	// application that stood is closed, its own name, its own database and its own
	// spelling of the event are free to compose.
	if err := live.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	t.Run("released", func(t *testing.T) {
		second, err := pkit.NewApp("collect").Use(doors, desk, postedModule[postedText]("ledger")).
			Build(t.Context(), buildDeployment(onOneDatabase(t), app.All))
		if err != nil {
			t.Fatalf("the refused application could not build once the live one closed: %v", err)
		}
		defer second.Close()
	})
}
