package tasktest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
)

// Review 16. `Op.Mutates` is what decides whether the retry and the four refusals
// — every "writes nothing" comparison the harness owns — are generated at all, and
// `Op.Snapshot` is what those comparisons render. Both are the module's own claim,
// and no case checks the one thing a reader of a description can check without
// running anything: that the operation which hands the harness a rendering of
// "everything this operation could have written" also claims it writes. An
// operation that supplies a Snapshot and denies mutating asks for an assertion the
// harness never runs — the retry and the refusals are generated from Mutates — so
// the field is read by no case, which is the rule this package's README opens with
// ("A field no case reads does not belong in the description") and house rule 7.
//
// The reachability check is the list itself: the description below is the one the
// suite runs, so the loop below walks real operations and not an empty slice.
//
// It passes as the port is described today, and reddens the day a command of this
// port starts writing from an operation still described as a read — which is the
// moment the generated cases stop covering it and somebody has to notice.
func TestEverySnapshotThePortHandsTheHarnessClaimsAWrite(t *testing.T) {
	suite := porttest.Names(tasktest.Suite(nil))
	if len(suite) == 0 {
		t.Fatal("tasktest.Suite describes no cases, so the loop below would assert nothing")
	}
	seen := 0
	for _, op := range tasktest.Suite(nil).Ops {
		if op.Snapshot == nil {
			continue
		}
		seen++
		if !op.Mutates {
			t.Errorf("%s supplies a Snapshot but claims Mutates=false: the harness generates its retry and its refusals from Mutates, so nothing compares the rendering it is handed. Either the operation writes, and it owes the cases a write owes, or it does not, and the Snapshot is a field no case reads", op.Name)
		}
	}
	if seen == 0 {
		t.Fatal("no operation of this port supplies a Snapshot, so this test would assert nothing")
	}
}
