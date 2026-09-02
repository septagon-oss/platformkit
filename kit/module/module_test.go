package module

import (
	"context"
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
