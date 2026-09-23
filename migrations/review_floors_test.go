package migrations_test

// review_floors_test.go is the case SPECIFY.md named as implement's —
// "migrations/floors_test.go asserts the two agree and that no floor is above the
// owner's head, so a raised floor is a review, not an edit" — and never wrote.
// migrations/README.md states the floors as measured and adds "Lowering a floor is
// a review, not an edit"; the only thing that made that sentence true was a
// temporary probe the implementation commit describes and then deleted.
//
// The agreement of the two doors needs no case: each manifest writes
// `RulesFrom: Migrations.RulesFrom`, one expression, so the module and its source
// cannot disagree. What nothing checked is that a floor is the number the SQL
// forces rather than the number that happens to work.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/migrations"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/user"
)

// unreachable is the connection the guard never needs: every rule and every header
// is judged in readMigrations, before the runner opens a pool, so a refused file
// refuses with its rule and an accepted file answers with the connect error.
const unreachable = "postgres://nobody:nothing@127.0.0.1:1/platformkit?sslmode=disable&connect_timeout=1"

// TestEachRuleFloorIsTheOneTheFilesForce. A floor too low refuses an applied file
// the release can no longer rewrite — which is a deploy that stops; a floor raised
// past a file the rules refuse is a rule quietly switched off for one owner, and
// nothing in the schema, the ledger or the diff would show it.
func TestEachRuleFloorIsTheOneTheFilesForce(t *testing.T) {
	owned := []db.MigrationSource{migrations.Source, user.Migrations, auth.Migrations, audit.Migrations}
	for _, source := range owned {
		t.Run(source.Owner, func(t *testing.T) {
			if source.RulesFrom < 1 {
				t.Fatalf("%s declares floor %d, which is one of the floors this repository states", source.Owner, source.RulesFrom)
			}
			// The floor as declared guards every file it claims to guard, and
			// refuses none of them: this is what every installation does on boot.
			if refusal := ruleRefusal(t, source, source.RulesFrom); refusal != "" {
				t.Errorf("%s at its declared floor %d is refused by the rule table: %s", source.Owner, source.RulesFrom, refusal)
			}
			// One lower, and a file that is already applied somewhere is judged by a
			// rule it cannot answer: the floor exists precisely for that file, and it
			// is the highest such file. A floor with nothing under it was raised.
			if refusal := ruleRefusal(t, source, source.RulesFrom-1); refusal == "" {
				t.Errorf("%s answers no rule refusal one version below its floor %d: nothing under the floor needs the exception, so the floor is larger than the files require and every version it skips is unguarded",
					source.Owner, source.RulesFrom)
			}
		})
	}
}

// ruleRefusal is the rule-table refusal this source would get with that floor, or
// the empty string when the guard is satisfied and the run got as far as the
// connection it never makes.
func ruleRefusal(t *testing.T, source db.MigrationSource, floor int64) string {
	t.Helper()
	source.RulesFrom = floor
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	err := db.Migrate(ctx, unreachable, source)
	if err == nil {
		t.Fatalf("%s migrated against %s, which is not a database", source.Owner, unreachable)
	}
	if strings.Contains(err.Error(), ": rule ") {
		return err.Error()
	}
	return ""
}
