package internal_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/history"
	"github.com/septagon-oss/platformkit/modules/history/contracts"
	"github.com/septagon-oss/platformkit/modules/history/internal"
)

// now is the instant every retention case ages from. The store never reads a
// clock of its own — Prune is handed the one its caller is working to — so a case
// that fixes this instant fixes the whole decision.
var now = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

// ago is one version's instant, this far before that clock.
func ago(d time.Duration) time.Time { return now.Add(-d) }

// TestTheRetentionRuleKeepsWhicheverOfItsTwoFloorsKeepsMore is the whole retention
// decision in one table: a version stands when either floor says so, so ninety days
// is not a ceiling on how much history a busy record keeps and fifty is not a floor
// under how little an idle one keeps. The two boundaries either side of ninety days
// are the case, because a rule that aged from "older than" rather than "at least
// as old as" would differ only there.
func TestTheRetentionRuleKeepsWhicheverOfItsTwoFloorsKeepsMore(t *testing.T) {
	rule := contracts.Defaults()
	cases := []struct {
		rank   int
		age    time.Duration
		keeps  bool
		reason string
	}{
		{1, 200 * 24 * time.Hour, true, "the newest stands however old it is"},
		{50, 200 * 24 * time.Hour, true, "the ceiling itself is kept"},
		{51, 200 * 24 * time.Hour, false, "past the ceiling and past the period"},
		{51, 10 * 24 * time.Hour, true, "past the ceiling but inside the period"},
		{52, 89*24*time.Hour + 23*time.Hour, true, "one hour short of the period"},
		{52, 90*24*time.Hour + time.Hour, false, "one hour past it"},
		{60, 90 * 24 * time.Hour, false, "the boundary is at least as old as the period"},
	}
	for _, c := range cases {
		if got := rule.Keep(c.rank, ago(c.age), now); got != c.keeps {
			t.Errorf("rank %d at %v: kept=%v, want %v (%s)", c.rank, c.age, got, c.keeps, c.reason)
		}
	}
}

// TestARuleThatKeepsNothingIsRefusedRatherThanRun is the boot answer for a rule
// that says something impossible: a period or a ceiling below one keeps no version
// that could be restored. Zero is not impossible — it is the composition saying
// nothing, and the defaults answer that — and a negative is a mis-set setting, which
// is refused rather than quietly read as the default it was not.
func TestARuleThatKeepsNothingIsRefusedRatherThanRun(t *testing.T) {
	if got := (contracts.Rule{}).Fault(); got != "" {
		t.Errorf("the absence of a rule is refused: %s", got)
	}
	if got := (contracts.Rule{Days: 1, Versions: 1}).Fault(); got != "" {
		t.Errorf("a rule that keeps something is refused: %s", got)
	}
	for _, bad := range []contracts.Rule{{Days: -1, Versions: 50}, {Days: 90, Versions: -1}} {
		if (contracts.Rule{Days: bad.Days, Versions: bad.Versions}).Fault() == "" {
			t.Errorf("retention of %d days and %d versions answers no fault", bad.Days, bad.Versions)
		}
	}
	if _, _, _, err := history.New(history.Deps{Retention: contracts.Rule{Days: -1}}); err == nil {
		t.Error("the module booted with a retention rule that keeps nothing")
	}
	if _, _, _, err := history.New(history.Deps{}); err != nil {
		t.Errorf("the module refused the rule its composition did not name: %v", err)
	}
}

// TestPruningRemovesTheVersionsItsRuleReleasedAndLeavesEveryOtherOneStanding is the
// sweep against a real table: the ranking is the record's own version order and the
// keeping is asked of the one predicate, so what goes is exactly what that decision
// released — and nothing else in the tenant, the series or the world moves.
func TestPruningRemovesTheVersionsItsRuleReleasedAndLeavesEveryOtherOneStanding(t *testing.T) {
	_, conn := dbtest.Schema(t, history.Migrations)
	svc := internal.NewService(nil)
	// Five versions of one record: the two newest, one young one past the ceiling,
	// and two that are both past the ceiling and past the period.
	series := []struct {
		number int64
		age    time.Duration
	}{
		{5, 0}, {4, time.Hour}, {3, 10 * 24 * time.Hour}, {2, 200 * 24 * time.Hour}, {1, 400 * 24 * time.Hour},
	}
	for _, s := range series {
		if err := keep(t, conn, acme, svc, versionAt(s.number, ago(s.age), "en", "Edit")); err != nil {
			t.Fatalf("keep version %d: %v", s.number, err)
		}
	}
	// A second record, whose versions the sweep has no business touching: every
	// one of them young, so its rule keeps them all.
	young := versionAt(1, ago(time.Minute), "en", "Another record's own")
	young.Record = uuid.New()
	if err := keep(t, conn, acme, svc, young); err != nil {
		t.Fatalf("keep the other record's version: %v", err)
	}
	otherRecord := young.Record

	removed := prune(t, conn, acme, svc, contracts.Rule{Days: 90, Versions: 2})
	if removed != 2 {
		t.Errorf("the sweep removed %d versions, want 2 (versions 1 and 2 of the first record)", removed)
	}

	read(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		items, err := svc.Page(ctx, tx, contracts.Query{
			Module: "content", Entity: "page", Locale: "en", Record: page, Limit: 20,
		})
		if err != nil {
			return err
		}
		if len(items) != 3 || items[0].Version != 5 || items[1].Version != 4 || items[2].Version != 3 {
			t.Errorf("the series stands at %s, want 5,4,3", versionsOf(items))
		}
		kept, err := svc.Page(ctx, tx, contracts.Query{
			Module: "content", Entity: "page", Locale: "en", Record: otherRecord, Limit: 20,
		})
		if err != nil {
			return err
		}
		if len(kept) != 1 {
			t.Errorf("the young record's series stands at %d versions, want 1", len(kept))
		}
		return nil
	})
}

// TestPruningReleasesTheFilesOfTheVersionsItRemovedAndNobodyElses is decision 0069
// §4's half this module can own: a retained version holds the pictures its body
// shows, so a version that stops being retained stops holding them, and the file it
// was the last to show becomes deletable. The release is asked of the fields the
// version itself filed — read back out of its row — because re-parsing the body at
// sweep time would be a second reading of the format, done at the worst moment.
func TestPruningReleasesTheFilesOfTheVersionsItRemovedAndNobodyElses(t *testing.T) {
	_, conn := dbtest.Schema(t, history.Migrations)
	ledger := &recorder{}
	svc := internal.NewService(ledger)
	first, second := uuid.New(), uuid.New()

	old := versionAt(1, ago(400*24*time.Hour), "en-GB", "Oldest")
	old.Refs = []contracts.Ref{{Field: "body", Files: []uuid.UUID{first}}}
	if err := keep(t, conn, acme, svc, old); err != nil {
		t.Fatalf("keep the oldest version: %v", err)
	}
	standing := versionAt(2, ago(time.Hour), "en-GB", "Newest")
	standing.Refs = []contracts.Ref{{Field: "body", Files: []uuid.UUID{second}}}
	if err := keep(t, conn, acme, svc, standing); err != nil {
		t.Fatalf("keep the newest version: %v", err)
	}
	if len(ledger.sets) != 2 {
		t.Fatalf("recording two versions filed %d uses, want one per version", len(ledger.sets))
	}

	removed := prune(t, conn, acme, svc, contracts.Rule{Days: 90, Versions: 1})
	if removed != 1 {
		t.Fatalf("the sweep removed %d versions, want 1", removed)
	}

	// The removed version released what it held; the standing version kept it.
	var releases int
	for _, s := range ledger.sets {
		if len(s.Files) == 0 {
			releases++
		}
	}
	if releases != 1 {
		t.Fatalf("the sweep wrote %d releases, want exactly one for the version it removed", releases)
	}
	last := ledger.sets[len(ledger.sets)-1]
	if len(last.Files) != 0 || last.Module != "history" || last.Entity != "version" || last.Field != "body" || last.Locale != "en-GB" {
		t.Fatalf("the sweep's last write to the ledger is %+v, want the removed version's body emptied in its own locale", last)
	}
	standingUses := 0
	for _, s := range ledger.sets {
		if len(s.Files) == 1 && s.Files[0] == second {
			standingUses++
		}
	}
	if standingUses != 1 {
		t.Errorf("the standing version's use was written %d times, want once and untouched", standingUses)
	}
}

// TestPruningOneTenantLeavesAnotherTenantsHistoryStanding is the sweep under
// tenancy: it runs per tenant, inside that tenant's transaction, and the versions it
// cannot see are versions it cannot remove. A DELETE that ignored the policy would
// empty a tenant whose own rule had not yet run.
func TestPruningOneTenantLeavesAnotherTenantsHistoryStanding(t *testing.T) {
	_, conn := dbtest.Schema(t, history.Migrations)
	svc := internal.NewService(nil)
	for _, tenant := range []tenancy.Tenant{acme, globex} {
		for _, number := range []int64{1, 2} {
			if err := keep(t, conn, tenant, svc, versionAt(number, ago(400*24*time.Hour), "en", "Ancient")); err != nil {
				t.Fatalf("keep %s's version %d: %v", tenant.Slug, number, err)
			}
		}
	}
	if got := prune(t, conn, acme, svc, contracts.Rule{Days: 90, Versions: 1}); got != 1 {
		t.Fatalf("Acme's sweep removed %d versions, want 1", got)
	}
	read(t, conn, globex, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		items, err := svc.Page(ctx, tx, contracts.Query{
			Module: "content", Entity: "page", Locale: "en", Record: page, Limit: 20,
		})
		if err != nil {
			return err
		}
		if len(items) != 2 {
			t.Errorf("Acme's sweep left Globex %d versions, want the two it had", len(items))
		}
		return nil
	})
	read(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		items, err := svc.Page(ctx, tx, contracts.Query{
			Module: "content", Entity: "page", Locale: "en", Record: page, Limit: 20,
		})
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].Version != 2 {
			t.Errorf("Acme's series stands at %s, want version 2 alone", versionsOf(items))
		}
		return nil
	})
}

// prune runs the sweep in one tenant's transaction and answers how many versions
// went, failing the case if the transaction itself refused.
func prune(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, svc *internal.Service, rule contracts.Rule) int {
	t.Helper()
	removed := -1
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		n, err := svc.Prune(ctx, tx, rule, now)
		removed = n
		return err
	})
	if err != nil {
		t.Fatalf("prune as %s: %v", tenant.Slug, err)
	}
	return removed
}

// recorder is the file ledger as this store sees it: which row asked for which
// files, in which field and language. It stands in for modules/file because the
// claim here is about which rows this store asks about, not about what a file is.
type recorder struct{ sets []rest.UsesInput }

func (r *recorder) SetUses(_ context.Context, _ db.Tx[db.Tenant], in rest.UsesInput) error {
	r.sets = append(r.sets, in)
	return nil
}
