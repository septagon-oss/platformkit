package module

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/jobs"
)

// TestValidateNamesEveryViolation: a composition is fixed once, so the first
// error is the least useful thing Validate could return.
func TestValidateNamesEveryViolation(t *testing.T) {
	mods := []Module{
		{
			Name:        "billing",
			Permissions: []Permission{{Key: "invoice:read"}},
			Events:      []string{"billing.invoice_issued"},
			Nav:         []NavEntry{{Label: "Invoices", Path: "/invoices", Permission: "invoice:read"}},
		},
		{
			Name:        "billing",
			Permissions: []Permission{{Key: "invoice:read"}},
			Events:      []string{"accounts.user_created"},
			Nav:         []NavEntry{{Label: "Reports", Path: "/reports", Permission: "report:read"}},
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
			Events:      []string{"accounts.user_created", "accounts.user_deleted"},
		},
		{
			Name: "billing",
			Nav:  []NavEntry{{Label: "Users", Path: "/users", Permission: "user:read"}},
		},
	}
	if err := Validate(mods); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := Validate(nil); err != nil {
		t.Fatalf("Validate(nil): %v", err)
	}
}

// TestValidateRejectsMalformedTokens: the grammar of a permission is the one
// kit/httpx enforces at the route, so a manifest cannot declare a key no route
// could ever require.
func TestValidateRejectsMalformedTokens(t *testing.T) {
	err := Validate([]Module{{
		Name:        "Billing",
		Permissions: []Permission{{Key: "Invoice.Read"}},
		Events:      []string{"nodot"},
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

// TestValidateRejectsAManifestThatCannotRun: a nil check panics inside /ready,
// so it is a composition error rather than a runtime surprise.
func TestValidateRejectsAManifestThatCannotRun(t *testing.T) {
	err := Validate([]Module{{Name: "billing", Health: []health.Check{nil}}})
	if err == nil {
		t.Fatal("Validate accepted a manifest that cannot run")
	}
	if !strings.Contains(err.Error(), "health check 0 is nil") {
		t.Errorf("error does not report the nil check\ngot:\n%v", err)
	}
}

// TestValidateChecksSubscriptionsAgainstWhatIsEmitted: a subscription to an
// event nobody publishes is a handler that will never run, and a manifest is
// where a person looks to find out whether it should.
func TestValidateChecksSubscriptionsAgainstWhatIsEmitted(t *testing.T) {
	handler := func(context.Context, db.Tx[db.Tenant], events.Event) error { return nil }

	// A subscription to another module's event is the ordinary case and passes.
	ok := []Module{
		{Name: "billing", Events: []string{"billing.invoice_issued"}},
		{Name: "ledger", Subscriptions: []events.Subscription{
			{Module: "ledger", Name: "billing.invoice_issued", Handler: handler},
		}},
	}
	if err := Validate(ok); err != nil {
		t.Fatalf("Validate refused a valid subscription: %v", err)
	}

	bad := []Module{
		{Name: "billing", Events: []string{"billing.invoice_issued"}},
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
	first := Module{Name: "first", Events: []string{"first.happened"}}
	// After the subscriber in the list, which is the case that used to be lost.
	last := Module{Name: "last", Events: []string{"last.happened", "last.again"}}

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
	want := []string{"first.happened", "last.again", "last.happened"}
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
