package app

// One app answers one event name under one payload shape while some composition holds a
// claim on it, and that claim is what the process-wide record of kit/events answers a
// publish with no Catalog on its context against (kit/events/catalog.go and the README's
// "which catalogue measures a publish"). New takes the claim, Start keeps it, and
// Runtime.Close gives it back, so the names a stopped composition declared are free for
// the next one. The boot that comes after a refusal, or after a Closed Runtime, therefore
// has to hold the claim again — claimDeclared says exactly that — because a composition
// running without one publishes payloads nothing measures and lets a second composition of
// the same app answer its names another way. This case asks one question at every step of
// one App's life: is a disagreeing declaration refused beside the standing composition,
// and does a publish on a bare context still reach the manifest its own module wrote?

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type restartedAmount struct {
	Total int64 `json:"total"`
}

// restartedText is the same event name spelled as a document that is not restartedAmount.
type restartedText struct {
	Total string `json:"total"`
}

const (
	restartedApp   = "rehearsal"
	restartedEvent = "rehearsal.invoice_issued"
	// restartedUndeclared is an event no module of this composition declared.
	restartedUndeclared = "rehearsal.nothing_happened"
)

func restartedBilling() module.Module {
	return module.Module{
		Name:     "rehearsal",
		Declared: []events.Declared{events.Declare[restartedAmount](restartedEvent)},
		// One route on the workspace surface: a composition that mounts nothing
		// there is refused at the boot, and this case is about the boots.
		Routes: func(s httpx.Surfaces) {
			httpx.Register(s.App, huma.Operation{OperationID: "rehearsalHold", Method: http.MethodGet, Path: "/hold"},
				httpx.Public(), func(context.Context, *struct{}) (*helloOut, error) {
					return &helloOut{}, nil
				})
		},
	}
}

// TestACompositionStartedAgainHoldsItsEventNamesAgain is the claim across one App's whole
// life: New takes it, a Closed Runtime gives it back, and the boot that comes after either
// a refusal or a Closed Runtime holds it again.
func TestACompositionStartedAgainHoldsItsEventNamesAgain(t *testing.T) {
	cfg, opts := compose(t)
	opts.App = appname.MustParse(restartedApp)
	other := []events.Declared{events.Declare[restartedText](restartedEvent)}
	slug := opts.App

	a, err := New(t.Context(), cfg, []module.Module{restartedBilling()}, opts)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	// Standing beside the composition New built, a disagreeing shape is refused.
	if err := events.CheckAppDeclared(slug, other); err == nil {
		t.Fatal("a composition that New built holds no claim on the names it declares")
	}

	rt, err := a.Start(t.Context())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// A tenant this composition holds, so that the tenant table and the composition
	// give one answer to "whose contract is this" throughout the case.
	admin := dbtest.Open(t, cfg.Database.MigrateURL)
	tenant := opts.Tenants.(fixture).tenant
	if _, err := admin.ExecContext(t.Context(),
		`INSERT INTO tenants (id, slug, name, status, app) VALUES ($1, $2, $2, 'active', $3)`,
		tenant.ID, tenant.Slug, restartedApp); err != nil {
		t.Fatalf("place the tenant: %v", err)
	}
	// publishBare is what the README calls a publish with nothing carried: a
	// transaction of the composition's own connection on a context no composition
	// put a door on — a goroutine that started from nowhere, a CLI command, a hook.
	publishBare := func(rt *Runtime, name string, body any) error {
		ctx := tenancy.WithTenant(t.Context(), tenant)
		return db.Run(ctx, rt.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, name, body)
		})
	}
	rows := func() int {
		var n int
		if err := admin.QueryRowContext(t.Context(),
			`SELECT count(*) FROM platformkit_outbox WHERE tenant_id = $1`, tenant.ID).Scan(&n); err != nil {
			t.Fatalf("count the tenant's outbox rows: %v", err)
		}
		return n
	}
	if err := events.CheckAppDeclared(slug, other); err == nil {
		t.Fatal("a started Runtime holds no claim on its composition's names")
	}
	if err := publishBare(rt, restartedEvent, restartedAmount{Total: 1200}); err != nil {
		t.Fatalf("the composition refused its own module's declared event: %v", err)
	}
	if n := rows(); n != 1 {
		t.Fatalf("the accepted publish left %d outbox rows, want one", n)
	}
	// A name no module declared is refused, which is the promise the write door makes
	// for a row no subscriber could ever receive.
	if err := publishBare(rt, restartedUndeclared, map[string]any{}); err == nil {
		t.Fatal("an undeclared event reached the outbox beside a live composition")
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// The stopped boot gave the names back: that is the disposal Close promises, and
	// what lets the next composition of this app mean what it says.
	if err := events.CheckAppDeclared(slug, other); err != nil {
		t.Fatalf("the closed Runtime kept its names: %v", err)
	}

	rt2, err := a.Start(t.Context())
	if err != nil {
		t.Fatalf("start the composition again: %v", err)
	}
	defer func() { _ = rt2.Close() }()

	if err := events.CheckAppDeclared(slug, other); err == nil {
		t.Error("the restarted composition runs with no claim on the event names it declares: " +
			"a second composition of the same app may answer one name under another payload beside it")
	}
	if err := publishBare(rt2, restartedEvent, restartedAmount{Total: 1300}); err != nil {
		t.Errorf("the restarted composition refused its own module's declared event on a bare context: %v", err)
	}
	if err := publishBare(rt2, restartedUndeclared, map[string]any{}); err == nil {
		t.Error("the restarted composition wrote an outbox row for an event no module of it declared")
	}
	// One row per accepted publish: each boot's declared event, and never the undeclared
	// one, which is refused and writes nothing.
	if n := rows(); n != 2 {
		t.Errorf("the two boots left %d outbox rows for this tenant, want one per declared event", n)
	}
}
