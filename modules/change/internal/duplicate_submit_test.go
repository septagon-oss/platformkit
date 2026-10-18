package internal_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/modules/change/internal"
)

// oneDiff is the change this case submits twice. It is parsed rather than written as
// a Go map so the numbers arrive the way an HTTP body delivers them.
func oneDiff(t *testing.T) contracts.Diff {
	t.Helper()
	return diff(t, `{"limit":11}`)
}

// waitingOnTheProposalIndex counts the sessions of this database that are stopped
// because a row another transaction has written conflicts with the one they are
// inserting. It is the same kind of observation
// modules/change/internal/concurrency_test.go makes of the row lock: a case that
// passed only because the machine was slow would say nothing about the collision, so
// the case waits until the collision is visible in the server before it lets the
// first submit finish.
func waitingOnTheProposalIndex(t *testing.T, admin *sql.DB) int {
	t.Helper()
	var n int
	if err := admin.QueryRow(`
		SELECT count(*) FROM pg_stat_activity
		WHERE datname = current_database()
		  AND state = 'active' AND wait_event_type = 'Lock'
		  AND query ILIKE '%change_proposals%'`).Scan(&n); err != nil {
		t.Fatalf("reading the lock queue: %v", err)
	}
	return n
}

// TestTwoSimultaneousSubmitsOfOneDiffSettleOnOneProposal is the screen that submits
// twice, submitted twice at once.
//
// The module promises the same answer whichever order the two arrive in. Read in
// order, Propose finds the open row and returns it with nothing published; the comment
// at the insert in internal/service.go says the partial unique index is "the answer to
// the race the duplicate check above cannot see: two inserts at once. The loser reads
// the winner's row and reports it, which is the same answer the sequential case
// gives", and modules/change/README.md repeats it: the index "is what makes a second
// submit of the same diff the same proposal rather than a race".
//
// Under a real race the loser does not read anything. Postgres answers a conflicting
// insert with 23505 and aborts the whole transaction; the recovery read then runs in
// that aborted transaction and fails too, so Propose returns the insert error and no
// row. The person who clicked twice gets a failure and no proposal id, which is the
// worse of the two available answers: they cannot tell whether their change is in the
// queue at all.
//
// What has to be true is what the sequential path already does: the second caller gets
// the winner's row, unchanged, and the tenant's outbox holds exactly one
// change.proposal_proposed for it. A savepoint around the insert, or the duplicate
// check taken under the subject's own row lock, both satisfy this. The assertion is
// about the answer the caller gets, not the mechanism that gets it there.
func TestTwoSimultaneousSubmitsOfOneDiffSettleOnOneProposal(t *testing.T) {
	admin, conn := dbtest.Schema(t, change.Migrations)
	subject := newCounter()
	svc := internal.NewService(bindingTo(subject))
	ctx := tenancy.WithTenant(context.Background(), acme)
	same := oneDiff(t)

	type outcome struct {
		row *contracts.Proposal
		err error
	}
	// The first submit parks between its insert and its commit, which is the only
	// window in which a second submit can collide with an open transaction.
	firstHeld := make(chan *contracts.Proposal, 1)
	releaseFirst := make(chan struct{})
	firstDone, secondDone := make(chan outcome, 1), make(chan outcome, 1)

	go func() {
		var row *contracts.Proposal
		err := db.Run(tenancy.WithActor(ctx, proposer), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var err error
			row, err = svc.Propose(ctx, tx, contracts.NewProposal{
				SubjectModule: "counter", SubjectEntity: "one",
				Diff: same, Summary: "raise the limit by one",
			})
			if err != nil {
				return err
			}
			firstHeld <- row
			<-releaseFirst
			return nil
		})
		firstDone <- outcome{row, err}
	}()
	held := <-firstHeld

	go func() {
		var row *contracts.Proposal
		err := db.Run(tenancy.WithActor(ctx, proposer), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var err error
			row, err = svc.Propose(ctx, tx, contracts.NewProposal{
				SubjectModule: "counter", SubjectEntity: "one",
				Diff: same, Summary: "raise the limit by one",
			})
			return err
		})
		secondDone <- outcome{row, err}
	}()

	// The second submit is only meaningful once it has actually collided with the
	// first; before that, this is a slow run of the sequential case.
	deadline := time.Now().Add(15 * time.Second)
	for waitingOnTheProposalIndex(t, admin) == 0 {
		select {
		case got := <-secondDone:
			t.Fatalf("the second submit finished before it ever collided: %v", got.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the second submit never collided with the first: this case cannot say what a collision does")
		}
		time.Sleep(20 * time.Millisecond)
	}

	close(releaseFirst)
	first, second := <-firstDone, <-secondDone
	if first.err != nil {
		t.Fatalf("the first submit: %v", first.err)
	}

	if second.err != nil {
		t.Errorf("the second submit of the same diff failed instead of returning the proposal that exists: %v", second.err)
	}
	if second.row == nil {
		t.Errorf("the second submit returned no row; the proposal that exists is %s", held.ID)
	} else if second.row.ID != held.ID {
		t.Errorf("the second submit returned %s; the proposal that exists is %s", second.row.ID, held.ID)
	}

	// One row and one event, whichever order the two submits settled in.
	var rows, events int64
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Raw(`SELECT count(*) FROM change_proposals`).Scan(&rows).Error; err != nil {
			return err
		}
		return tx.DB().Raw(`SELECT count(*) FROM platformkit_outbox WHERE name = ?`,
			contracts.EventProposed).Scan(&events).Error
	}); err != nil {
		t.Fatalf("counting what survived: %v", err)
	}
	if rows != 1 {
		t.Errorf("one diff submitted twice produced %d open proposals", rows)
	}
	if events != 1 {
		t.Errorf("one diff submitted twice published %d %s events", events, contracts.EventProposed)
	}
	if subject.saves != 0 || subject.revision != 7 {
		t.Errorf("proposing wrote the subject: %d saves, revision %d (%s)",
			subject.saves, subject.revision, json.RawMessage(subject.current))
	}
}
