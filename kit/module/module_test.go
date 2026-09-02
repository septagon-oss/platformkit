package module

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/health"
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
			Permissions: []Permission{{Key: "user:read", Description: "See users"}},
			Events:      []string{"accounts.user_created", "accounts.user_deleted"},
		},
		{
			Name: "billing",
			Nav:  []NavEntry{{Label: "Users", Path: "/users", Permission: "user:read", Order: 1}},
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

// TestValidateRejectsAManifestThatCannotRun: a nil check panics inside /ready
// and a job with no schedule is work that silently never happens, so both are
// composition errors rather than runtime surprises.
func TestValidateRejectsAManifestThatCannotRun(t *testing.T) {
	err := Validate([]Module{{
		Name:   "billing",
		Health: []health.Check{nil},
		Jobs:   []Job{{Name: "sweep"}},
	}})
	if err == nil {
		t.Fatal("Validate accepted a manifest that cannot run")
	}
	for _, want := range []string{"health check 0 is nil", `job "sweep" needs a name, a schedule`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not report %s\ngot:\n%v", want, err)
		}
	}
}
