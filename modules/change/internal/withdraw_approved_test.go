package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/modules/change/internal"
)

// TestAnApprovedProposalIsTheProposersToWithdraw covers the two rows of SPECIFY §3.3
// nothing reached before this file: rule 2's "withdraw from `proposed` *or* `approved`"
// and rule 5's "withdrawing an approved proposal makes the diff unappliable; re-reviewing
// it needs a new proposal". `Withdraw` asks `Proposal.Open()`, the queue's question, which
// takes in `approved` on purpose: the row has a verdict but the decision has not been
// carried out, and taking the change back is the proposer's call until it is. The two
// commands that would undo a decision already made — `Review`, `Apply` — refuse, and the
// same bytes then come back as a second proposal rather than as a reopened first one, so
// the record of the first opinion survives.
func TestAnApprovedProposalIsTheProposersToWithdraw(t *testing.T) {
	_, conn := dbtest.Schema(t, change.Migrations)
	subject := newCounter()
	svc := internal.NewService(bindingTo(subject))
	var id = uuid.Nil
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		author := tenancy.WithActor(ctx, proposer)
		decider := tenancy.WithActor(ctx, reviewer)
		row := propose(t, subject, svc, author, tx)
		id = row.ID
		approved, err := svc.Review(decider, tx, id, contracts.Review{Verdict: contracts.VerdictApproved, ExpectedRevision: 1})
		if err != nil {
			return err
		}
		withdrawn, err := svc.Withdraw(author, tx, id, approved.Revision)
		if err != nil {
			return err
		}
		if withdrawn.State != contracts.StateWithdrawn || withdrawn.Revision != 3 {
			t.Errorf("withdrawing an approved proposal left %+v", withdrawn)
		}
		if withdrawn.Verdict != contracts.VerdictApproved || withdrawn.Reviewer == nil || *withdrawn.Reviewer != reviewer {
			t.Errorf("a withdrawn proposal lost the verdict it was withdrawn away from: %+v", withdrawn)
		}
		// Rule 5's first half: the verdict stands on the record, the write never happens.
		if _, err := svc.Apply(decider, tx, id, withdrawn.Revision); !errors.Is(err, crud.ErrConflict) {
			t.Errorf("applying a withdrawn proposal returned %v, and only an approved one applies", err)
		}
		// Rule 5's second half, asked twice over. A verdict nobody wrote yet — the
		// reviewer's own reversal, or another decider's approval — is refused: the row is
		// withdrawn, and re-reviewing it needs a new proposal, which is the next block.
		for _, try := range []struct {
			who     uuid.UUID
			verdict string
			why     string
		}{
			{reviewer, contracts.VerdictDeclined, "the reviewer reversing their verdict on a withdrawn proposal"},
			{secondDecider, contracts.VerdictApproved, "another decider approving a withdrawn proposal"},
		} {
			if _, err := svc.Review(tenancy.WithActor(ctx, try.who), tx, id, contracts.Review{
				Verdict: try.verdict, ExpectedRevision: withdrawn.Revision,
			}); !errors.Is(err, crud.ErrConflict) {
				t.Errorf("%s returned %v, and rule 5 wants a new proposal", try.why, err)
			}
		}
		// What a withdrawn row does answer is rule 1 rather than rule 5: the verdict the
		// row already carries, replayed by the account that wrote it, is the row as it
		// stands — no transition, no event, still withdrawn at revision 3. A replay is not
		// a re-decision, and this is the one answer a client that lost its response gets.
		replay, err := svc.Review(decider, tx, id, contracts.Review{
			Verdict: contracts.VerdictApproved, ExpectedRevision: withdrawn.Revision,
		})
		if err != nil || replay == nil || replay.State != contracts.StateWithdrawn || replay.Revision != 3 {
			t.Errorf("the reviewer's replay of the verdict they wrote returned %+v, %v; want the withdrawn row at revision 3", replay, err)
		}
		if subject.saves != 0 {
			t.Errorf("the withdrawn round wrote the subject %d times, and nothing was applied", subject.saves)
		}
		again, err := svc.Propose(author, tx, contracts.NewProposal{
			SubjectModule: "counter", SubjectEntity: "one",
			Diff: diff(t, `{"limit":11}`), Summary: "the same change, put forward again",
		})
		if err != nil {
			return err
		}
		if again.ID == id || again.State != contracts.StateProposed || again.Revision != 1 || again.BaseRevision != 7 {
			t.Errorf("the same diff after a withdrawal came back as %+v, want a new row awaiting its first decision", again)
		}
		if got := published(t, tx); len(got) != 4 || got[2] != contracts.EventWithdrawn || got[3] != contracts.EventProposed {
			t.Errorf("the withdrawn round published %v, want proposed, reviewed, withdrawn, proposed", got)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("the case's transaction: %v", err)
	}
}
