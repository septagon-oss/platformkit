package main

// repair_catalogue_join_test.go pins the repair's half of the join review 15
// named and closed from the seeder's side: the catalogue `repair-roles` narrows
// against has to be the composition's own list, and not a second list that
// happens to agree with it today.
//
// The command takes grants away. It decides which ones by asking which of the
// seeded grants no composed module defines any more, so the list it is handed is
// the boundary of its permission: too wide and it leaves a dead grant where it
// reported a repair, too narrow and a live permission reads as departed and the
// command takes from an administrator a grant every route in the installation
// still honours. The narrow direction is the one that destroys something, and it
// is exactly the shape a composition reaches by holding two lists — the one the
// seeding hook reads, which is the live `mods` of compose and grows after the hook
// is wired, and the one this struct keeps, which is a snapshot of that variable at
// the moment compose returns. Two texts that say `module.Grants(...)` are two
// answers to one question and nothing in the language notices one of them drifting
// — which is how this application seeded `billing:catalog` after the module that
// defines it left. So compose now keeps one closure (composition.catalogue), and
// the hook and the command are handed that value, as they are handed initialRoles.
//
// What this case can therefore check, and what nothing else does, is that the
// value both seams read is the composition's manifest list read independently:
// the expectation below is written out from `c.modules` rather than computed by
// the function under test, because what is forbidden here is a catalogue that is
// a list somebody wrote, or one taken before the modules arrived. The behavioural
// half of the same claim — the command run against an installation with nothing
// to repair leaves every row, including the operator's own administrator, alone —
// is review 7's TestARepairOfAnInstallationThatNeedsNoRepairChangesNothing, which
// an empty catalogue fails; the half that asks the *running installation* whether
// it defines the seeded grants is review 15's
// TestEveryGrantTheSeederWroteIsOneTheRunningInstallationDefines, which reaches
// the guard over HTTP. This is the link between those two: what the guard answers
// about is the list the command narrows against.
import (
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestTheRepairNarrowsAgainstTheCatalogueItsSeederWasGiven(t *testing.T) {
	_, cfg := configure(t)
	c := compose(cfg)

	// The expectation, written out from the manifests this composition holds.
	// Nothing in this file's production code computes it.
	var composed []tenancy.Grant
	for _, m := range c.modules {
		for _, p := range m.Permissions {
			composed = append(composed, tenancy.Grant{Permission: p.Key, Operator: p.Operator})
		}
	}

	catalogue := c.catalogue()
	if len(catalogue) == 0 {
		t.Fatal("the catalogue the seeder and the repair read together answers empty: the closure in compose is the one taken before mods was filled, and this command would read every operator grant in the installation as departed")
	}
	if !slices.Equal(catalogue, composed) {
		t.Errorf("the catalogue both seams read is not the manifest list of the modules this composition holds: catalogue %+v, composed %+v",
			catalogue, composed)
	}
	// The list is read, not spent: the hook runs per tenant created and the
	// command reads it once, so a second answer that differs from the first is
	// a catalogue with state in it.
	if again := c.catalogue(); !slices.Equal(again, catalogue) {
		t.Errorf("the catalogue answered %v the first time and %v the second", catalogue, again)
	}
	// A catalogue with no operator permission in it is the unfilled closure
	// wearing a full list: this composition composes the tenant module, which
	// defines one (see TestTheBootstrapSeedsWhatThisFileComposes for the same
	// guard on the seeding seam).
	if len(authcontracts.OperatorGrants(catalogue)) == 0 {
		t.Fatal("the catalogue the repair narrows against names no operator permission, so no seeded operator grant could be one it recognises")
	}

	// And that one list is the list the request guard was declared with, which
	// is what makes the repair's boundary and the installation's agree: a
	// permission the guard routes but this list lacks would be a live grant this
	// command takes away, and the mismatch is named by permission rather than by
	// count. kit/app expands the composition before it declares anything
	// (kit/app buildAPI), and Expand rewriting permissions is what
	// TestExpandRewritesSubscriptionsAndNothingElse forbids at the source; this
	// is the same join seen from the composition that seeds and repairs.
	guard := module.Grants(module.Expand(c.modules))
	for _, g := range guard {
		if !slices.Contains(catalogue, g) {
			t.Errorf("the installation defines %q and the catalogue the repair narrows against does not: a role holding it would have the grant taken from it by this command", g.Permission)
		}
	}
	for _, g := range catalogue {
		if !slices.Contains(guard, g) {
			t.Errorf("the catalogue the repair narrows against defines %q and the installation does not: the seeder would write a grant the running guard refuses", g.Permission)
		}
	}
}
