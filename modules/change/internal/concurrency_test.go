package internal_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/modules/change/internal"
)

// guarded is the subject of the concurrency case: counter's shape, plus the only two
// observations a lock has to be able to give anybody — whether two writers were ever
// writing it at once, and how many writes it took in total. Nothing else in this package
// can see a lock: the row it protects is real Postgres, and the subject is the test's own.
//
// The write window is the one that matters. Lock is asked by the read that decides (by
// Propose for its base revision, and by Apply for the staleness answer), so a writer that
// took Lock and wrote nothing would count as an overlap under a coarser flag — and Propose
// does exactly that, two transactions before the one this case is about.
//
// hold parks a writer inside that window — after the proposal row is locked, before the
// subject is written — until the test lets it go. That window is where the second apply has
// to be stopped, so it is the window the case holds open.
type guarded struct {
	mu       sync.Mutex
	enter    sync.Once
	writing  bool
	overlaps int
	saves    int
	current  json.RawMessage
	revision int64

	hold    chan struct{}
	entered chan struct{}
}

func (g *guarded) Lock(context.Context, db.Tx[db.Tenant]) (json.RawMessage, int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.current, g.revision, nil
}

func (g *guarded) Save(_ context.Context, _ db.Tx[db.Tenant], merged json.RawMessage) (int64, error) {
	g.mu.Lock()
	if g.writing {
		g.overlaps++
	}
	g.writing = true
	g.enter.Do(func() { close(g.entered) })
	hold := g.hold
	g.mu.Unlock()

	if hold != nil {
		<-hold
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	g.saves++
	g.current = merged
	g.revision++
	g.writing = false
	return g.revision, nil
}

func (g *guarded) tally() (saves, overlaps int, revision int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.saves, g.overlaps, g.revision
}

func guardedBinding(subject *guarded) []contracts.SubjectBinding {
	return []contracts.SubjectBinding{{
		Module: "counter", Entity: "one",
		Resolve: func(context.Context, db.Tx[db.Tenant], uuid.UUID) (contracts.Subject, error) {
			return subject, nil
		},
	}}
}

// waitingForUpdate counts the sessions of this database that are active and stopped
// on a lock while running a SELECT ... FOR UPDATE. It is how the case below knows the
// second apply reached the row and stopped there rather than merely being slow: an
// assertion that passes because the machine was slow is an assertion about timing, and
// timing is exactly what a missing row lock also looks like.
func waitingForUpdate(t *testing.T, admin *sql.DB) int {
	t.Helper()
	var n int
	if err := admin.QueryRow(`
		SELECT count(*) FROM pg_stat_activity
		WHERE datname = current_database()
		  AND state = 'active' AND wait_event_type = 'Lock'
		  AND query ILIKE '%FOR UPDATE%'`).Scan(&n); err != nil {
		t.Fatalf("reading the lock queue: %v", err)
	}
	return n
}

// TestTwoAppliesOfOneProposalNeverShareTheSubject is the FOR UPDATE case: two deciders,
// one approved proposal, one revision both of them read. Ask the revision check alone
// and both are told "yes"; the row lock is the only thing that makes one of them wait,
// and a lock that is merely written down rather than taken is invisible to every other
// case here.
//
// The first apply is parked inside the subject while holding the proposal row. The
// second has to queue behind that row — the case refuses unless it sees the queue in
// pg_stat_activity — and then the subject itself, which counts its own writers, answers
// whether anybody was ever inside it twice.
//
// The first apply commits, because the question is what the second one reads after the
// row is released: an uncommitted first apply would leave the second free to succeed, and
// "the waiter was refused" and "the waiter was allowed through" would be told apart by
// nothing. Each case here gets its own schema, so committing costs the next case nothing.
//
// Replacing crud.GetForUpdate with crud.Get in Service.open — measured, see the Verified
// paragraph of this file's commit — leaves the second apply with nothing to wait for, no
// queue ever forms in pg_stat_activity, and this case fails over it within the ten seconds
// a wait for a lock that never arrives is worth. The subject's own writer count is the
// second half of the same claim and stays unexercised by that mutation: the case stops
// earlier, at the question it asked first.
func TestTwoAppliesOfOneProposalNeverShareTheSubject(t *testing.T) {
	admin, conn := dbtest.Schema(t, change.Migrations)
	subject := &guarded{
		current:  json.RawMessage(`{"label":"before","limit":10}`),
		revision: 7,
		hold:     make(chan struct{}),
		entered:  make(chan struct{}),
	}
	svc := internal.NewService(guardedBinding(subject))

	// The proposal is approved by a reviewer who is not the proposer, because an
	// apply by the proposer is refused for another reason and this case is about the
	// lock. Revision 2 is what both deciders below will claim to have read.
	var id uuid.UUID
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		ctx = tenancy.WithActor(ctx, proposer)
		row, err := svc.Propose(ctx, tx, contracts.NewProposal{
			SubjectModule: "counter", SubjectEntity: "one",
			Diff: diff(t, `{"limit":11}`), Summary: "raise the limit by one",
		})
		if err != nil {
			return err
		}
		id = row.ID
		ctx = tenancy.WithActor(ctx, reviewer)
		_, err = svc.Review(ctx, tx, row.ID, contracts.Review{
			Verdict: contracts.VerdictApproved, ExpectedRevision: row.Revision,
		})
		return err
	}); err != nil {
		t.Fatalf("approving the proposal the two applies race about: %v", err)
	}

	decider := func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		ctx = tenancy.WithActor(ctx, reviewer)
		_, err := svc.Apply(ctx, tx, id, 2)
		return err
	}

	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- db.Run(tenancy.WithTenant(t.Context(), acme), conn, decider) }()

	select {
	case <-subject.entered:
	case <-time.After(60 * time.Second):
		t.Fatal("the first apply never reached the subject")
	}

	go func() { second <- db.Run(tenancy.WithTenant(t.Context(), acme), conn, decider) }()

	deadline := time.Now().Add(10 * time.Second)
	for waitingForUpdate(t, admin) == 0 {
		if time.Now().After(deadline) {
			close(subject.hold)
			t.Fatal("the second apply never queued behind the proposal row: nothing here can say whether the lock is taken")
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(subject.hold)

	// The first writer was the one holding the row, so it is the one that gets to write.
	if err := <-first; err != nil {
		t.Fatalf("the apply that held the row: %v", err)
	}
	// The waiter read the revision it was told about, found the row moved under it, and is
	// refused — which is the whole of what the lock is for: without it, both of these
	// answers would be "yes".
	if err := <-second; !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("the apply that queued for the row: %v", err)
	}

	var state string
	var rowRevision, appliedRevision int64
	var events []string
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := crud.Get[*contracts.Proposal](tx, id)
		if err != nil {
			return err
		}
		state, rowRevision, appliedRevision = row.State, row.Revision, row.AppliedRevision
		events = published(t, tx)
		return nil
	}); err != nil {
		t.Fatalf("reading the row back: %v", err)
	}
	if state != contracts.StateApplied || rowRevision != 3 || appliedRevision != 8 {
		t.Errorf("two applies left the proposal %s at revision %d applied on %d; one approval is one transition",
			state, rowRevision, appliedRevision)
	}
	if len(events) != 3 {
		t.Errorf("the trail holds %v; the refused apply published nothing, so three transitions is three events", events)
	}

	saves, overlaps, revision := subject.tally()
	if overlaps != 0 {
		t.Errorf("two applies were writing the subject at once %d time(s): the proposal row does not serialize them", overlaps)
	}
	if saves != 1 {
		t.Errorf("the subject was written %d time(s) by two applies of one proposal, and one approval is one write", saves)
	}
	if revision != 8 {
		t.Errorf("the subject ended on revision %d; one write onto revision 7 is revision 8", revision)
	}
}
