package porttest

import (
	"testing"

	"github.com/google/uuid"
)

// Review 8. Two assertions of this package have no witness anywhere in the
// repository, so each is one deleted comparison away from a green tree over a
// fake that breaks it. Both were measured by mutation, not by reading:
//
//	world.go   `if now := w.Events(w.Fixture); len(now) != said {`  ->  `false && …`
//	                                                             ok x4, whole suite green
//	porttest.go  the tenant case's second watch, of the row Provoke returned
//	                                                             ok x4, whole suite green
//
// The first is house rule 9's "a refused command emits nothing": README lists
// `Suite.Events` as read by "every refusal ("a refusal is not news")", and
// `World.Refused` says "House rule 9's first two clauses as one assertion" — the
// second of those two clauses is the one nothing watches. The sibling assertions
// are all watched: review 6's pin watches the error identity, review 7's the
// snapshot movement, porttest_test.go's `emitAlways` the *retry's* silence. Only
// a refused call that spoke anyway is unobserved, and it is the case every
// mutating operation of every port runs.
//
// The second is the row Provoke hands back. `TestTenantCaseFailsWhenTheRowWas
// TakenAwayDuringProvoke` deletes the row the case seeded and returns that same
// row, so it is covered by the watch taken at seeding; a Provoke that returns a
// *different* row — a setup that found the row to act on rather than the one the
// case minted — is watched by nothing, and losing it during a refused call is the
// same defect with the row identity changed.
//
// Both tests pass at this commit: the assertions exist and fire. They fail under
// the two mutations above, which is the whole point — they are the witness those
// two comparisons lacked, not a complaint about behaviour this branch has.

const tenantCaseName = "File: " + string(Elsewhere)

// TestRefusalCaseFailsWhenTheRefusedCallSpoke is the witness for "a refusal is
// not news": a fake that refuses and emits anyway has to fail the refusal case
// for that reason, and for that reason alone — it touches no row, so the
// snapshot half of the assertion stays silent.
func TestRefusalCaseFailsWhenTheRefusedCallSpoke(t *testing.T) {
	// The passing branch first: a refusal that says nothing passes its case.
	quiet := noteSuite(knobs{})
	quiet.Own = nil
	if said := watch(t, quiet).failures(tenantCaseName); len(said) > 0 {
		t.Errorf("%q failed over a fake that refused and said nothing: %v", tenantCaseName, said)
	}

	suite := noteSuite(knobs{})
	suite.Own = nil
	for i, r := range suite.Ops[0].Refusals {
		if r.Kind != Denied {
			continue
		}
		r.Call = func(w world, row uuid.UUID) error {
			_, err := w.service.File(w.stranger, row, revisionOf(w, row))
			// The bug this case exists for: the call was refused and spoke
			// anyway. A subscriber hears a note filed that was never filed.
			if emit := w.service.Events.Emit("note.filed"); emit != nil {
				t.Fatalf("emitting the event the port declares: %v", emit)
			}
			return err
		}
		suite.Ops[0].Refusals[i] = r
	}
	log := watch(t, suite)
	log.mustFail(t, "File: a caller with no grant is refused and writes nothing", "a refusal is not news")
}

// TestTenantCaseWatchesTheRowProvokeReturned is the witness for the second watch.
// Provoke seeds a second row and hands *that* one to the refused call; the broken
// copy answers the visitor's call with the refusal it owes and takes that row away
// from the tenant that owns it. The refusal is correct, the snapshot is read after
// Provoke and so says "no such note" either side of the call, and the seeded row is
// untouched — so every comparison the description owns is satisfied, and only the
// watch taken on the row Provoke returned can see the loss.
func TestTenantCaseWatchesTheRowProvokeReturned(t *testing.T) {
	suite := noteSuite(knobs{erases: true})
	suite.Own = nil
	for i, r := range suite.Ops[0].Refusals {
		if r.Kind != Elsewhere {
			continue
		}
		r.Provoke = func(t *testing.T, w world, _ uuid.UUID) uuid.UUID {
			t.Helper()
			return w.seed(t, false) // a different row than the one the case seeded
		}
		suite.Ops[0].Refusals[i] = r
	}
	log := watch(t, suite)
	log.mustFail(t, tenantCaseName, "still be there for the tenant that owns it")
}
