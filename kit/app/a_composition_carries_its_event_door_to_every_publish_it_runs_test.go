package app

// The composition owns two halves, and a publish can arrive at either: the request the
// router serves and the work the worker runs. Both are started by kit/app over resources
// no module holds a handle to — the pool, the transport, the tenant scope — which is why
// the composition's event door reaches them on the context rather than through a module's
// wiring (kit/events/publisher.go, and the httpx.WithConn precedent in kit/httpx).
//
// What these cases check is the install rather than the mechanism: a composition whose
// tenant the tenant table says somebody else holds still measures its own publishes
// against its own manifest, and a subscription handler finds that same door on the
// context its delivery runs on.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
)

type servedInvoice struct {
	Total int64 `json:"total"`
}

const (
	servedEvent = "billing.invoice_issued"
	servedPath  = "/api/v1/billing/issue"
)

// servedBilling is a module with one route that publishes its own event on the request's
// transaction — the way a module writes one, the plain package call and no wiring — under
// either the payload it declared or one that is not it.
func servedBilling(good bool) module.Module {
	return module.Module{
		Name:     "billing",
		Declared: []events.Declared{events.Declare[servedInvoice](servedEvent)},
		Routes: func(s httpx.Surfaces) {
			httpx.Register(s.App, huma.Operation{OperationID: "issue", Method: http.MethodPost, Path: "/issue"},
				httpx.Public(), func(ctx context.Context, _ *struct{}) (*helloOut, error) {
					tx, ok := httpx.TxFrom(ctx)
					if !ok {
						return nil, errors.New("the handler has no transaction")
					}
					var body any = servedInvoice{Total: 4200}
					if !good {
						body = map[string]any{"total": "not minor units"}
					}
					if err := events.Publish(ctx, tx, servedEvent, body); err != nil {
						return nil, err
					}
					out := &helloOut{}
					out.Body.Tenant = db.TenantOf(tx).Slug
					return out, nil
				})
		},
	}
}

// askServed answers one request of a started composition.
func askServed(t *testing.T, rt *Runtime, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://"+tenantHost+path, nil)
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestAServedRequestIsMeasuredByTheCompositionThatServesIt puts the composition's tenant
// under another app's slug, and asks the served route to publish. Measured against the
// tenant's placement the event is undeclared and the payload unchecked; measured against
// the composition that is serving it, the valid payload commits and the malformed one is
// refused inside the request's own transaction.
func TestAServedRequestIsMeasuredByTheCompositionThatServesIt(t *testing.T) {
	cfg, opts := compose(t)
	tenant := opts.Tenants.(fixture).tenant
	a, err := New(t.Context(), cfg, []module.Module{servedBilling(true)}, opts)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	rt, err := a.Start(t.Context())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	admin := dbtest.Open(t, cfg.Database.MigrateURL)
	if _, err := admin.ExecContext(t.Context(),
		`INSERT INTO tenants (id, slug, name, status, app) VALUES ($1, $2, $2, 'active', $3)`,
		tenant.ID, tenant.Slug, "somewhere-else"); err != nil {
		t.Fatalf("place the tenant under another app: %v", err)
	}

	status, body := askServed(t, rt, servedPath)
	if status != http.StatusOK {
		t.Fatalf("the composition refused its own valid event: %d %s", status, body)
	}
	var rows int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE tenant_id = $1 AND name = $2`, tenant.ID, servedEvent).Scan(&rows); err != nil {
		t.Fatalf("count the served event: %v", err)
	}
	if rows != 1 {
		t.Fatalf("the served request left %d outbox rows, want one", rows)
	}

	// The same route, one field spelled wrong: refused by this composition's own
	// declaration, with nothing written.
	malformed, err := New(t.Context(), cfg, []module.Module{servedBilling(false)}, opts)
	if err != nil {
		t.Fatalf("compose the malformed route: %v", err)
	}
	bad, err := malformed.Start(t.Context())
	if err != nil {
		t.Fatalf("start the malformed route: %v", err)
	}
	defer bad.Close()
	status, body = askServed(t, bad, servedPath)
	if status == http.StatusOK {
		t.Fatal("the composition served a payload it declared minor units for")
	}
	if !strings.Contains(body, servedEvent) && !strings.Contains(body, "500") {
		t.Logf("the refused request answered %d: %s", status, body)
	}
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE tenant_id = $1 AND name = $2`, tenant.ID, servedEvent).Scan(&rows); err != nil {
		t.Fatalf("count the served event again: %v", err)
	}
	if rows != 1 {
		t.Errorf("the refused request left %d outbox rows, want the served one only", rows)
	}
}

// carriedDoor is a transport that keeps the context each subscription is made on: the
// worker half's own, the one it hands to every delivery afterwards.
type carriedDoor struct {
	mu   sync.Mutex
	ctxs []context.Context
}

func (c *carriedDoor) Publish(context.Context, events.Event) error { return nil }

func (c *carriedDoor) Subscribe(ctx context.Context, _, _ string, _ events.Sink) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ctxs = append(c.ctxs, ctx)
	return nil
}

// Observe refuses: this recorder counts subscriptions and watches nothing.
func (c *carriedDoor) Observe(context.Context, uuid.UUID, string, func(events.Event)) (func(), error) {
	return nil, errors.New("events: this transport double observes nothing")
}

func (c *carriedDoor) contexts() []context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]context.Context(nil), c.ctxs...)
}

// TestTheWorkerHalfCarriesItsCompositionsEventDoorToItsSubscriptions reads the context
// the worker half subscribes on — the one every delivery, job tick and relay step derives
// from — and asks whether the composition's door is on it. A handler that hands its
// context to a goroutine is only still publishing under its own composition if the door
// came with the context.
func TestTheWorkerHalfCarriesItsCompositionsEventDoorToItsSubscriptions(t *testing.T) {
	cfg, opts := compose(t)
	opts.Role = Worker
	door := &carriedDoor{}
	opts.Transport = door
	a, err := New(t.Context(), cfg, []module.Module{
		{Name: "billing", Declared: []events.Declared{events.Declare[servedInvoice](servedEvent)},
			Subscriptions: []events.Subscription{{
				Module: "billing", Name: servedEvent,
				Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error { return nil },
			}}},
	}, opts)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	rt, err := a.Start(t.Context())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- rt.Work(ctx) }()

	// The worker subscribes once per subscription, on the context it runs them all
	// on; the first one the recorder saw is that context.
	var carried context.Context
	wait := time.NewTicker(20 * time.Millisecond)
	defer wait.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for carried == nil {
		got := door.contexts()
		if len(got) > 0 {
			carried = got[0]
			break
		}
		select {
		case err := <-stopped:
			t.Fatalf("the worker half stopped before subscribing: %v", err)
		case <-deadline.C:
			t.Fatal("the worker half never subscribed, so it never had a context to carry")
		case <-wait.C:
		}
	}
	pub, ok := events.PublisherFrom(carried)
	if !ok {
		t.Fatal("a delivery context carries no event door: the worker half publishes under whatever the process last installed")
	}
	if pub != rt.Publisher() {
		t.Error("the carried door belongs to another composition")
	}
	cancel()
	<-stopped
}
