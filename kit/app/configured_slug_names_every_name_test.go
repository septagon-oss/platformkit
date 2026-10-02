package app

// The app a composition is has two sources: nats.app, the setting every shared name
// is formed from (migrations/000041 places tenants by it, kit/events/providers/nats
// addresses its subjects with it, apps/platformkit stamps new tenants with it), and
// Options.App, the same fact spoken by whoever wires in code. Read separately they
// answer one question twice, and a deployment that named itself only in
// configuration — the documented way, and the way the reference application composes
// — got two answers: its tenants were collect, its subjects were collect, and its
// catalog, relay claim, subscription durables and job lock were nobody's.
//
// kit/app.New answers it once. These are the two branches of that answer, measured at
// the doors the composition actually runs through: the durable a subscription is
// created under, the outbox rows the worker relays, and the boot that refuses itself
// rather than hold both spellings.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
)

// slugWorker is the transport the composition hands its worker: it keeps the consumer
// name each subscription bound under and the ids the outbox relay published, so the
// test reads the names the composition formed rather than asserting a field.
type slugWorker struct {
	mu        sync.Mutex
	durables  []string
	published []string
	signal    chan struct{}
}

func (w *slugWorker) Publish(_ context.Context, ev events.Event) error {
	w.mu.Lock()
	w.published = append(w.published, ev.Name)
	w.mu.Unlock()
	select {
	case w.signal <- struct{}{}:
	default:
	}
	return nil
}

func (w *slugWorker) Subscribe(_ context.Context, durable, _ string, _ events.Sink) error {
	w.mu.Lock()
	w.durables = append(w.durables, durable)
	w.mu.Unlock()
	select {
	case w.signal <- struct{}{}:
	default:
	}
	return nil
}

func (w *slugWorker) seen() (durables []string, published []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.durables...), append([]string(nil), w.published...)
}

// TestAConfiguredSlugNamesTheCompositionsOwnWork composes with nats.app alone — no
// Options.App, exactly as the reference application composes — and asks the two doors
// the worker runs through: does the subscription bind under this app's durable, and
// does the relay carry the outbox rows of the tenants this app holds.
func TestAConfiguredSlugNamesTheCompositionsOwnWork(t *testing.T) {
	const configured = "collect"
	cfg, opts := compose(t)
	if err := db.Migrate(t.Context(), cfg.Database.MigrateURL, migrations.Source); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	admin := dbtest.Open(t, cfg.Database.MigrateURL)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()

	tenantID := uuid.New()
	if _, err := admin.ExecContext(t.Context(),
		`INSERT INTO tenants (id, slug, name, app) VALUES ($1, 'configured-customer', 'Configured customer', $2)`,
		tenantID, configured); err != nil {
		t.Fatalf("place a tenant under the configured slug: %v", err)
	}
	billing := module.Module{
		Name:   "billing",
		Events: []string{"billing.plan.created"},
		Subscriptions: []events.Subscription{{
			Module: "billing", Name: "billing.plan.created",
			Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error { return nil },
		}},
	}
	cfg.NATS.App = configured
	opts.Role = Worker
	worker := &slugWorker{signal: make(chan struct{}, 32)}
	opts.Transport = worker

	a, err := New(t.Context(), cfg, []module.Module{billing, brand("configuredslug", "")}, opts)
	if err != nil {
		t.Fatalf("compose the deployment whose app is configured, not coded: %v", err)
	}
	rt, err := a.Start(t.Context())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer rt.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- rt.Work(ctx) }()

	workCtx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: tenantID, Slug: "configured-customer"})
	if err := db.Run(workCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, "billing.plan.created", map[string]any{"plan": "starter"})
	}); err != nil {
		t.Fatalf("write the tenant's event: %v", err)
	}

	// The outbox relay runs on a one-second tick, so the wait is for the tick plus the
	// bind, not for a delivery the transport would make: this transport keeps what it
	// is handed. A composition whose relay claimed nothing would sit out the whole
	// window and be reported as one.
	deadline := time.After(20 * time.Second)
	for {
		_, published := worker.seen()
		if len(published) > 0 {
			break
		}
		select {
		case <-worker.signal:
		case <-deadline:
			durables, _ := worker.seen()
			t.Fatalf("the worker composed from nats.app=%q relayed nothing; it subscribed %v, so this is the app-less pass over a tenant its app holds",
				configured, durables)
		}
	}
	durables, published := worker.seen()
	if len(published) != 1 || published[0] != "billing.plan.created" {
		t.Errorf("the worker relayed %v, want only this tenant's one billing.plan.created", published)
	}
	want := appname.Durable(configured, "billing", "billing.plan.created")
	var bound []string
	for _, durable := range durables {
		if durable == want {
			bound = append(bound, durable)
		}
	}
	if len(bound) == 0 {
		t.Errorf("the composition whose app is configured as %q subscribed consumer %v: the durable is the name that says which app owns a delivery, and it carries the slug the tenants and the subjects already carry",
			configured, durables)
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Work did not return when its context was done")
	}
}

// TestACompositionNamingTwoAppsIsRefused is the other branch: an option and a setting
// that each name an app are two answers, and the boot that keeps both serves one app
// and relays another.
func TestACompositionNamingTwoAppsIsRefused(t *testing.T) {
	for _, test := range []struct {
		about     string
		setting   string
		option    appname.Name
		wantInErr []string
	}{
		{
			about:     "the option and the setting name different apps",
			setting:   "collect",
			option:    appname.MustParse("acme"),
			wantInErr: []string{"Options.App", "acme", "nats.app", "collect"},
		},
		{
			about:     "the setting is present and not a slug",
			setting:   "Collect Team",
			wantInErr: []string{"nats.app"},
		},
		{
			about:   "the option and the setting name one app",
			setting: "collect",
			option:  appname.MustParse("collect"),
		},
		{
			about:   "only the option names an app",
			setting: "",
			option:  appname.MustParse("acme"),
		},
	} {
		t.Run(test.about, func(t *testing.T) {
			cfg, opts := compose(t)
			cfg.NATS.App = test.setting
			opts.App = test.option
			_, err := New(t.Context(), cfg, nil, opts)
			if len(test.wantInErr) == 0 {
				if err != nil {
					t.Fatalf("New with nats.app=%q and Options.App=%q: %v", test.setting, test.option, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("New with nats.app=%q and Options.App=%q passed the boot gate", test.setting, test.option)
			}
			for _, want := range test.wantInErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("New with nats.app=%q and Options.App=%q = %v, want it to name %q",
						test.setting, test.option, err, want)
				}
			}
		})
	}
}
