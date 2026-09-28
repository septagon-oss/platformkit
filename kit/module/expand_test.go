package module

import (
	"context"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
)

// TestExpandRewritesSubscriptionsAndNothingElse holds the line Expand's own
// comment claims, which is the one claim about this package that another
// package's correctness rests on and that no composition can test for itself.
//
// Two catalogues of one composition are read in two places, and nothing compares
// them at run time: kit/app declares Grants of the *expanded* list to the request
// guard, which is the door that refuses a role naming a permission nobody owns
// (kit/app buildAPI, httpx.API.Declare), and a composition hands Grants of the
// list it still holds — unexpanded, because it expanded nothing — to whatever
// seeds a tenant's roles and to whatever repairs what that seeding wrote. As
// long as Expand leaves permissions alone, those are two names for one answer to
// "does this installation define this permission". Mint a permission here and a
// grant a seeder wrote becomes one the running installation answers 422 for,
// which is a grant that grants nothing; drop one here and the repair reading the
// unexpanded list reads a live permission as departed and takes it away from an
// administrator. Both directions are a few lines to check at the cause and
// expensive to discover in one composition's cases, which is why the assertion
// lives here rather than in a product's fixtures.
func TestExpandRewritesSubscriptionsAndNothingElse(t *testing.T) {
	handler := func(context.Context, db.Tx[db.Tenant], events.Event) error { return nil }
	mods := []Module{
		{
			Name:        "billing",
			Permissions: []Permission{{Key: "invoice:read"}, {Key: "invoice:issue", Operator: true}},
			Events:      []string{"billing.invoice_issued", "billing.invoice_paid"},
			Nav:         []NavEntry{{Label: "Invoices", Screen: "billing/invoices", Permission: "invoice:read"}},
		},
		// The one manifest Expand rewrites, and deliberately between the two
		// that declare permissions: an expansion that reached past its own
		// module would show up in the neighbour's list below.
		{Name: "trail", SubscribeAll: true,
			Subscriptions: []events.Subscription{{Module: "trail", Handler: handler}}},
		{Name: "tenant", Permissions: []Permission{{Key: "tenant:manage", Operator: true}}},
	}

	got := Expand(mods)
	if len(got) != len(mods) {
		t.Fatalf("Expand returned %d modules for a composition of %d", len(got), len(mods))
	}
	// The expansion really expanded: without this the six claims below could
	// all be satisfied by an Expand that returns its argument untouched.
	if len(got[1].Subscriptions) != 2 || got[1].SubscribeAll {
		t.Fatalf("the trail holds %d subscriptions and SubscribeAll %v: this case needs a module whose one template became one per event",
			len(got[1].Subscriptions), got[1].SubscribeAll)
	}

	// The join itself: one catalogue, whichever list Grants is handed.
	if before, after := Grants(mods), Grants(got); !slices.Equal(before, after) {
		t.Errorf("the catalogue of a composition is not the catalogue of its expansion: expanded %v, given %v",
			after, before)
	}
	for i, m := range mods {
		if got[i].Name != m.Name {
			t.Fatalf("position %d is module %q after expansion and %q before", i, got[i].Name, m.Name)
		}
		if !slices.Equal(got[i].Permissions, m.Permissions) {
			t.Errorf("module %q defines %v after expansion and %v before", got[i].Name, got[i].Permissions, m.Permissions)
		}
		if !slices.Equal(got[i].Events, m.Events) {
			t.Errorf("module %q emits %v after expansion and %v before", got[i].Name, got[i].Events, m.Events)
		}
		if !slices.Equal(got[i].Nav, m.Nav) {
			t.Errorf("module %q navigates %v after expansion and %v before", got[i].Name, got[i].Nav, m.Nav)
		}
	}
	// The argument is not modified, including by the permission question above:
	// a composition holds this list and seeds from it.
	if len(mods[1].Subscriptions) != 1 || !mods[1].SubscribeAll {
		t.Error("Expand rewrote the manifest it was given")
	}
}
