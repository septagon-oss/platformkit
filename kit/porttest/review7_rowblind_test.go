package porttest

import (
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// Review 7. `snapshotSeesTheRow` exempts a row `Ready` does not name — a per-tenant
// singleton whose Snapshot takes no row argument, which is why sitetest passes the
// witness without satisfying it. Review 6 recorded that it could not test the
// exemption itself, only what the exemption is supposed to leave intact, and left
// that under Unverified. This file is the test it could not write: a world whose
// Ready names no row at all, driven through the harness with the module's own
// fake landing its write before the refusal.
//
// It pins the one thing the exemption must not cost such a port. The witness is
// skipped for a row nobody named, and the natural way to "finish" that exemption
// is to stop rendering a snapshot for a row nobody named either — at which point
// "a refused call wrote nothing" compares two constants and a singleton fake can
// write on every refusal of the port while the suite stays green. Nothing else in
// this repository would notice: tasktest and contenttest pass a real row, and the
// shipped singleton fake refuses without writing, so the only witness that the
// exemption leaves the comparison alive is this one.
//
// Both branches are real. The suite passes whole over a fake that refuses without
// touching the row (TestRowBlindWorldRunsItsCases), and the refusal case fails for
// its own reason over the fake that does not (TestRowBlindWorld...WritesNothing).

// rowBlindWorld remembers, per service, the row its Ready seeded and then refused
// to name. A per-tenant singleton keeps exactly one row per tenant, so one entry
// per world is the whole store.
type rowBlindWorld struct {
	mu   sync.Mutex
	seed map[*notes]uuid.UUID
}

func (r *rowBlindWorld) put(service *notes, row uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seed[service] = row
}

func (r *rowBlindWorld) of(service *notes) (uuid.UUID, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.seed[service]
	return row, ok
}

// rowBlindSuite describes the same File port as noteSuite, in the shape a
// per-tenant singleton has: the operation, its refusals and its snapshot all reach
// the row through the world, because Ready names no row for the harness to pass
// along. This is sitetest's shape, not tasktest's.
func rowBlindSuite(k knobs) Suite[world] {
	suite := noteSuite(k)
	suite.Own = nil
	store := &rowBlindWorld{seed: map[*notes]uuid.UUID{}}
	op := &suite.Ops[0]

	op.Ready = func(t *testing.T, w world) uuid.UUID {
		t.Helper()
		store.put(w.service, w.seed(t, false))
		return uuid.Nil // the tenant's own row: Ready names none, so the witness is exempt
	}
	// The whole world this singleton has: its one row, rendered without looking at
	// the row argument, because there is no row argument to look at.
	op.Snapshot = func(t *testing.T, w world, _ uuid.UUID) string {
		t.Helper()
		row, ok := store.of(w.service)
		if !ok {
			return "nothing stored"
		}
		stored, err := w.service.Note(w.ctx, row)
		if err != nil {
			return "no such note"
		}
		return fmt.Sprintf("filed=%v sealed=%v rev=%d", stored.Filed, stored.Sealed, stored.Rev)
	}
	op.Call = func(w world, _ uuid.UUID) (string, error) {
		row, _ := store.of(w.service)
		filed, err := w.service.File(w.ctx, row, revisionOf(w, row))
		return fmt.Sprintf("filed=%v rev=%d", filed.Filed, filed.Rev), err
	}

	// The grant case, because it is the refusal whose write this pin watches. The
	// other three are declined in writing rather than described: each needs a row
	// identity the singleton does not hand out, and this file is about the
	// comparison the exemption must not swallow.
	var refusals []Refusal[world]
	for _, r := range op.Refusals {
		if r.Kind == Denied {
			refusals = append(refusals, Refusal[world]{
				Kind: Denied,
				Call: func(w world, _ uuid.UUID) error {
					row, _ := store.of(w.service)
					_, err := w.service.File(w.stranger, row, revisionOf(w, row))
					return err
				},
				Is: r.Is,
			})
		}
	}
	op.Refusals = refusals
	op.Skip = map[Kind]string{
		Unknown:   "the singleton has no row argument to lose, so an unknown id is not a call this port can make",
		Stale:     "the singleton takes no revision, which TestTheStaleSkipReasonStillStatesWhatTheKernelHas watches",
		Elsewhere: "this pin is about the snapshot comparison; the tenant case is the subject of the tenant pins",
	}
	return suite
}

const (
	rowBlindSuccess = "File: the operation says what it did"
	rowBlindDenied  = "File: a caller with no grant is refused and writes nothing"
)

// TestRowBlindWorldRunsItsCases is the passing branch: the exemption is not a
// refusal to run a case. Over a fake that refuses without touching the store, the
// row-blind description runs its cases and passes them.
func TestRowBlindWorldRunsItsCases(t *testing.T) {
	log := watch(t, rowBlindSuite(knobs{}))
	if said := log.failures(rowBlindSuccess); len(said) > 0 {
		t.Errorf("%q failed over a fake that is right: %v", rowBlindSuccess, said)
	}
	if said := log.failures(rowBlindDenied); len(said) > 0 {
		t.Errorf("%q failed over a fake that is right: %v", rowBlindDenied, said)
	}
	if len(log.names()) < 2 {
		t.Errorf("the row-blind suite ran %v; a world whose Ready names no row still owes its cases", log.names())
	}
}

// TestRowBlindRefusalCaseFailsWhenTheWriteLands is the bite: the same world, with
// the fake landing the write before it refuses. "A refused call wrote nothing" has
// to still be an assertion in a world whose row nobody named.
func TestRowBlindRefusalCaseFailsWhenTheWriteLands(t *testing.T) {
	log := watch(t, rowBlindSuite(knobs{writeFirst: true}))
	log.mustFail(t, rowBlindDenied, "a refused call wrote")
}
