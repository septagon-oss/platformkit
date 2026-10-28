package main

import "testing"

// TestEachPrintedRefusalCarriesTheApplicationClauseOfItsOwnLine reads the application
// clause through one answer that refuses several imports at once. The clause says whose
// reach a line blames, so it belongs to one line and to nothing else in the answer: it
// stands before a refusal only while that refusal's own rule refuses a reach into a
// module's internals and that refusal's own package sits in an apps/ tree. A prefix
// chosen once for the whole answer — from its first refusal, or from any refusal inside
// it — prints the clause beside a module's own import or drops it from an application's
// reach, and a reading of one refusal's line still matches its substring either way.
// Inputs: one call over an application's reach into a module's internals, a module's own
// reach into the same internals and the application's helper import, in both orders, each
// line carrying the advice its own rule holds. Failure: an answer whose lines do not each
// carry the clause their own refusal describes.
func TestEachPrintedRefusalCarriesTheApplicationClauseOfItsOwnLine(t *testing.T) {
	const rule, helperRule = "foreign-internal", "test-helper-in-production"
	internals := foundation + "/modules/ledger/internal/ledger"
	helper := foundation + "/modules/ledger/contracts/ledgertest"
	module := foundation + "/modules/billing/internal/stripe"
	application := foundation + "/apps/platformkit/composition"
	applicationReached := "OUT OF BOUNDS [" + rule + "] apps/ reaches into: " + application + " -> " +
		internals + " (production): " + remedies[rule] + "\n"
	moduleReached := "OUT OF BOUNDS [" + rule + "] " + module + " -> " + internals +
		" (production): " + remedies[rule] + "\n"
	helperImported := "OUT OF BOUNDS [" + helperRule + "] " + application + " -> " + helper +
		" (production): " + remedies[helperRule] + "\n"
	applicationReach := refusal{edge: edge{from: application, to: internals, ownerPath: application, context: "production"}, rule: rule}
	moduleReach := refusal{edge: edge{from: module, to: internals, ownerPath: module, context: "production"}, rule: rule}
	helperReach := refusal{edge: edge{from: application, to: helper, ownerPath: application, context: "production"}, rule: helperRule}
	for _, tc := range []struct {
		name    string
		refused []refusal
		want    string
	}{
		{"application first", []refusal{applicationReach, moduleReach, helperReach}, applicationReached + moduleReached + helperImported},
		{"module first", []refusal{moduleReach, applicationReach, helperReach}, moduleReached + applicationReached + helperImported},
	} {
		if printed := diagnostics(tc.refused); printed != tc.want {
			t.Errorf("%s must print exactly %q, printed %q", tc.name, tc.want, printed)
		}
	}
}
