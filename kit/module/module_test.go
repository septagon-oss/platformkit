package module

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/jobs"
)

// TestValidateNamesEveryViolation: a composition is fixed once, so the first
// error is the least useful thing Validate could return.
func TestValidateNamesEveryViolation(t *testing.T) {
	mods := []Module{
		{
			Name:        "billing",
			Permissions: []Permission{{Key: "invoice:read"}},
			Declared:    []events.Declared{{Name: "billing.invoice_issued"}},
			Nav:         []NavEntry{{Label: "Invoices", Screen: "billing/invoices", Permission: "invoice:read"}},
		},
		{
			Name:        "billing",
			Permissions: []Permission{{Key: "invoice:read"}},
			Declared:    []events.Declared{{Name: "accounts.user_created"}},
			Nav:         []NavEntry{{Label: "Reports", Screen: "billing/reports", Permission: "report:read"}},
		},
	}
	err := Validate(mods)
	if err == nil {
		t.Fatal("Validate accepted a composition with four violations")
	}
	for _, want := range []string{
		`"billing": declared twice`,
		`permission "invoice:read" is already defined by module "billing"`,
		`event "accounts.user_created" is not namespaced by the module that emits it`,
		`nav entry "Reports" requires permission "report:read", which no module defines`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not report %s\ngot:\n%v", want, err)
		}
	}
}

// TestValidateAcceptsAWellFormedComposition, including a nav entry that points
// at another module's permission: a link is about what the reader may see.
func TestValidateAcceptsAWellFormedComposition(t *testing.T) {
	mods := []Module{
		{
			Name:        "accounts",
			Permissions: []Permission{{Key: "user:read"}},
			Declared:    []events.Declared{{Name: "accounts.user_created"}, {Name: "accounts.user_deleted"}},
		},
		{
			Name: "billing",
			Nav:  []NavEntry{{Label: "Users", Screen: "accounts/users", Permission: "user:read"}},
		},
	}
	if err := Validate(mods); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := Validate(nil); err != nil {
		t.Fatalf("Validate(nil): %v", err)
	}
}

// TestValidateRefusesAdoptionWithoutMigrations: an adoption names files, so a
// manifest that adopts and has no files is a declaration nothing can check.
func TestValidateRefusesAdoptionWithoutMigrations(t *testing.T) {
	err := Validate([]Module{{Name: "orders", Adopts: []db.Adoption{{Owner: "platformkit", Versions: []int64{2}}}}})
	if err == nil || !strings.Contains(err.Error(), `module "orders": adopts migration history and declares no Migrations`) {
		t.Fatalf("Validate = %v", err)
	}
}

// TestValidateRejectsMalformedTokens: the grammar of a permission is the one
// kit/httpx enforces at the route, so a manifest cannot declare a key no route
// could ever require.
func TestValidateRejectsMalformedTokens(t *testing.T) {
	err := Validate([]Module{{
		Name:        "Billing",
		Permissions: []Permission{{Key: "Invoice.Read"}},
		Declared:    []events.Declared{{Name: "nodot"}},
	}})
	if err == nil {
		t.Fatal("Validate accepted a malformed module")
	}
	for _, want := range []string{
		`module "Billing": a name is a lower-case identifier`,
		`permission "Invoice.Read" is not "<resource>:<action>"`,
		`event "nodot" is not "<name>.<event>"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not report %s\ngot:\n%v", want, err)
		}
	}
}

// TestValidateChecksSubscriptionsAgainstWhatIsEmitted: a subscription to an
// event nobody publishes is a handler that will never run, and a manifest is
// where a person looks to find out whether it should.
func TestValidateChecksSubscriptionsAgainstWhatIsEmitted(t *testing.T) {
	handler := func(context.Context, db.Tx[db.Tenant], events.Event) error { return nil }

	// A subscription to another module's event is the ordinary case and passes.
	ok := []Module{
		{Name: "billing", Declared: []events.Declared{{Name: "billing.invoice_issued"}}},
		{Name: "ledger", Subscriptions: []events.Subscription{
			{Module: "ledger", Name: "billing.invoice_issued", Handler: handler},
		}},
	}
	if err := Validate(ok); err != nil {
		t.Fatalf("Validate refused a valid subscription: %v", err)
	}

	bad := []Module{
		{Name: "billing", Declared: []events.Declared{{Name: "billing.invoice_issued"}}},
		{Name: "ledger", Subscriptions: []events.Subscription{
			{Module: "ledger", Name: "billing.invoice_voided", Handler: handler},
			{Module: "ledger", Name: "billing.invoice_issued"},
			{Module: "audit", Name: "billing.invoice_issued", Handler: handler},
		}},
	}
	err := Validate(bad)
	if err == nil {
		t.Fatal("Validate accepted three broken subscriptions")
	}
	for _, want := range []string{
		`subscribes to "billing.invoice_voided", which no module emits`,
		`subscription to "billing.invoice_issued" has no handler`,
		`is attributed to module "audit"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not report %s\ngot:\n%v", want, err)
		}
	}
}

// TestValidateChecksJobs: a job whose schedule does not parse is a job that
// never fires, which is a composition error and not a runtime surprise.
func TestValidateChecksJobs(t *testing.T) {
	err := Validate([]Module{{Name: "billing", Jobs: []jobs.Job{
		{Name: "sweep", Cron: "every hour", Run: func(context.Context, *db.Conn) error { return nil }},
	}}})
	if err == nil || !strings.Contains(err.Error(), "five-field cron expression") {
		t.Errorf("Validate = %v, want the unparseable schedule", err)
	}
}

// TestSubscribeAllHearsAModuleComposedAfterIt is the whole reason the field
// exists. main used to compute the list of events and hand it to the
// subscriber as a dependency, which was correct only while the subscriber was
// composed last — and a module listed after it was a module nothing recorded,
// silently. The kernel has every manifest before it expands anything, so where
// a module sits in the list cannot change what is heard.
func TestSubscribeAllHearsAModuleComposedAfterIt(t *testing.T) {
	var heard []string
	record := func(_ context.Context, _ db.Tx[db.Tenant], ev events.Event) error {
		heard = append(heard, ev.Name)
		return nil
	}
	trail := Module{
		Name:          "trail",
		SubscribeAll:  true,
		Subscriptions: []events.Subscription{{Module: "trail", Handler: record}},
	}
	first := Module{Name: "first", Declared: []events.Declared{{Name: "first.happened"}}}
	// After the subscriber in the list, which is the case that used to be lost.
	last := Module{Name: "last", Declared: []events.Declared{{Name: "last.happened"}, {Name: "last.again"}}}

	got := Expand([]Module{first, trail, last})
	if len(got) != 3 {
		t.Fatalf("Expand returned %d modules, want three", len(got))
	}
	var names []string
	for _, s := range got[1].Subscriptions {
		if s.Module != "trail" || s.Handler == nil {
			t.Errorf("subscription %+v lost its module or its handler", s)
		}
		names = append(names, s.Name)
	}
	// Every event a module declares, and the events the kernel emits itself (KernelEvents),
	// sorted: the trail hears a refused authorization like any other event.
	want := slices.Sorted(slices.Values(append([]string{"first.happened", "last.again", "last.happened"}, KernelEvents...)))
	if !slices.Equal(names, want) {
		t.Errorf("the trail subscribes to %v, want %v", names, want)
	}
	// The argument is untouched, and the expanded copy no longer asks.
	if len(trail.Subscriptions) != 1 || !trail.SubscribeAll {
		t.Error("Expand modified the manifest it was given")
	}
	if got[1].SubscribeAll {
		t.Error("the expanded manifest still asks to be expanded")
	}
	// And it is a composition the kernel accepts, which the unexpanded one is
	// not: a subscription with no name names no event anybody emits.
	if err := Validate(got); err != nil {
		t.Errorf("the expanded composition is invalid: %v", err)
	}

	// The handler is the one that was declared, once per event.
	for _, s := range got[1].Subscriptions {
		if err := s.Handler(t.Context(), db.Tx[db.Tenant]{}, events.Event{Name: s.Name}); err != nil {
			t.Fatalf("the handler: %v", err)
		}
	}
	if !slices.Equal(heard, want) {
		t.Errorf("the handler heard %v, want %v", heard, want)
	}
}

// TestSubscribeAllTakesExactlyOneSubscription: the name is what the kernel
// fills in, so a second subscription beside the template is one the expansion
// would silently ignore.
func TestSubscribeAllTakesExactlyOneSubscription(t *testing.T) {
	handler := func(context.Context, db.Tx[db.Tenant], events.Event) error { return nil }
	err := Validate([]Module{{
		Name: "trail", SubscribeAll: true,
		Subscriptions: []events.Subscription{
			{Module: "trail", Handler: handler},
			{Module: "trail", Handler: handler},
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "SubscribeAll") {
		t.Errorf("Validate = %v, want the refusal", err)
	}
}

// TestTheKernelManifestIsTheOnlyOneThatMayEmitAKernelEvent: KernelEvents names
// what the kernel raises itself, and the exemption from the namespace rule is
// bounded to the one manifest that carries them. A module named "billing" that
// declared security.denied would be a module emitting an event it does not raise,
// which is the same violation under the other spelling; a kernel manifest reaching
// for a module's name would be the same thing backwards.
func TestTheKernelManifestIsTheOnlyOneThatMayEmitAKernelEvent(t *testing.T) {
	t.Run("the kernel's own manifest may declare one", func(t *testing.T) {
		if err := Validate([]Module{{Name: KernelName, Declared: []events.Declared{{Name: KernelEvents[0]}}}}); err != nil {
			t.Errorf("Validate refused the kernel's own event: %v", err)
		}
	})
	t.Run("another module may not", func(t *testing.T) {
		err := Validate([]Module{{Name: "billing", Declared: []events.Declared{{Name: KernelEvents[0]}}}})
		if err == nil || !strings.Contains(err.Error(), "is not namespaced by the module that emits it") {
			t.Errorf("Validate = %v, want the namespace refusal for a module that emits a kernel event", err)
		}
	})
	t.Run("and the kernel's manifest may not reach for a module's", func(t *testing.T) {
		err := Validate([]Module{{Name: KernelName, Declared: []events.Declared{{Name: "billing.invoice_issued"}}}})
		if err == nil || !strings.Contains(err.Error(), "is not namespaced by the module that emits it") {
			t.Errorf("Validate = %v, want the namespace refusal for a kernel manifest outside KernelEvents", err)
		}
	})
}

// TestAManifestMayNameAnEventWithoutNamingItsPayload: Events is the name-only
// spelling of the same list. A module whose payload the kernel cannot describe —
// a hand-built document, a type that marshals itself — still owns its events, is
// namespace-checked on them, is counted as an emitter, and is expanded for a
// SubscribeAll subscriber, because every one of those rules reads Module.Emits
// rather than either field. A manifest that reached for another module's name
// here would be the same violation the typed spelling refuses, and a name given
// both ways is one event, described.
func TestAManifestMayNameAnEventWithoutNamingItsPayload(t *testing.T) {
	named := Module{Name: "work", Events: []string{"work.task_changed"}}
	if err := Validate([]Module{named}); err != nil {
		t.Errorf("Validate refused a manifest that names its event without its payload: %v", err)
	}
	if got := events.Names(named.Emits()); !slices.Contains(got, "work.task_changed") {
		t.Errorf("Emits returned %v, want the bare name among what the module emits", got)
	}

	err := Validate([]Module{{Name: "work", Events: []string{"billing.invoice_issued"}}})
	if err == nil || !strings.Contains(err.Error(), "is not namespaced by the module that emits it") {
		t.Errorf("Validate = %v, want the namespace refusal for a bare name outside the module", err)
	}

	t.Run("a bare name reaches a SubscribeAll subscriber", func(t *testing.T) {
		listener := Module{
			Name:          "trail",
			SubscribeAll:  true,
			Subscriptions: []events.Subscription{{Module: "trail", Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error { return nil }}},
		}
		got := Expand([]Module{named, listener})
		var heard []string
		for _, s := range got[1].Subscriptions {
			heard = append(heard, s.Name)
		}
		if !slices.Contains(heard, "work.task_changed") {
			t.Errorf("the subscriber hears %v, which does not include the event named without a payload", heard)
		}
	})

	t.Run("a name given both ways is one event, described", func(t *testing.T) {
		both := Module{
			Name:     "work",
			Events:   []string{"work.task_changed"},
			Declared: []events.Declared{events.Declare[string]("work.task_changed")},
		}
		emitted := both.Emits()
		if len(emitted) != 1 {
			t.Fatalf("Emits returned %d events for one name given twice, want one", len(emitted))
		}
		if emitted[0].Payload == nil {
			t.Error("the bare name stood in for the typed declaration that was given beside it")
		}
	})
}
