package main

import (
	"testing"
)

// TestOnlyAnApplicationReachesIntoWhatItRefused reads the clause of a printed refusal
// that says who made the refused import: the gate prefixes "apps/ reaches into: "
// before the refusing package only while that package sits in an apps/ tree, and
// prints the pair alone for a module's own import, because an application is the only
// source allowed to hold a composition that reaches a dependency's internals. The
// prefix is chosen by the refusing package's path and by nothing else, so the two
// readings differ in that clause alone, and a widened condition would tell the author
// of a module that their application is at fault. Inputs: the refusals this rule raises
// in a production build over a module's internal package and over the same import from
// an application's construction, each printed through diagnostics with the advice the
// remedy map holds for the rule. Failure: a printed line that is not the refusal's own
// pair, context and advice, or a prefix standing on a module's import or missing from
// an application's.
func TestOnlyAnApplicationReachesIntoWhatItRefused(t *testing.T) {
	const rule = "foreign-internal"
	advice := remedies[rule]
	target := foundation + "/modules/ledger/contracts"
	module := foundation + "/modules/billing/internal/stripe"
	application := foundation + "/apps/platformkit/composition"
	for _, tc := range []struct {
		name, from, want string
	}{
		{
			name: "a module's own import",
			from: module,
			want: "OUT OF BOUNDS [foreign-internal] " + module + " -> " + target + " (production): " + advice + "\n",
		},
		{
			name: "an application's import",
			from: application,
			want: "OUT OF BOUNDS [foreign-internal] apps/ reaches into: " + application + " -> " + target + " (production): " + advice + "\n",
		},
	} {
		printed := diagnostics([]refusal{{
			edge: edge{from: tc.from, to: target, ownerPath: tc.from, context: "production"},
			rule: rule,
		}})
		if printed != tc.want {
			t.Errorf("%s must print exactly %q, printed %q", tc.name, tc.want, printed)
		}
	}
}
