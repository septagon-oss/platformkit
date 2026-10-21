package internal_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
	"github.com/septagon-oss/platformkit/modules/decide/contracts/decidetest"
)

// A vote racing the creator's close either lands before the close and is kept,
// or observes the close and is refused with nothing written: the ballot row and
// the decide.voted event exist exactly when the vote was accepted, and the
// question closes exactly once.
func TestAVoteRacingACloseLandsBeforeItOrIsRefusedWithoutAWrite(t *testing.T) {
	for round := range 4 {
		w := newWorld(t)
		w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return put(ctx, tx, decidetest.Seed()) })
		start := make(chan struct{})
		var voteErr, closeErr error
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			voteErr = db.Run(w.ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := w.svc.Vote(ctx, tx, contracts.VoteInput{QuestionID: decidetest.Q, OptionID: decidetest.A, Value: contracts.Yes})
				return err
			})
		})
		wg.Go(func() {
			<-start
			closeErr = db.Run(w.ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := w.svc.Close(ctx, tx, contracts.QuestionInput{QuestionID: decidetest.Q, IfRevision: 1})
				return err
			})
		})
		close(start)
		wg.Wait()
		if closeErr != nil {
			t.Fatalf("round %d: close: %v", round, closeErr)
		}
		if voteErr != nil && !errors.Is(voteErr, contracts.ErrClosed) {
			t.Fatalf("round %d: vote: %v", round, voteErr)
		}
		var ballots, voted, closed int
		var status string
		if err := w.admin.QueryRowContext(t.Context(), `SELECT
			(SELECT count(*) FROM decide_votes WHERE value IS NOT NULL),
			(SELECT count(*) FROM platformkit_outbox WHERE name=$1),
			(SELECT count(*) FROM platformkit_outbox WHERE name=$2),
			(SELECT status FROM decide_questions WHERE id=$3)`, contracts.EventVoted, contracts.EventClosed, decidetest.Q).Scan(&ballots, &voted, &closed, &status); err != nil {
			t.Fatal(err)
		}
		accepted := 0
		if voteErr == nil {
			accepted = 1
		}
		if ballots != accepted || voted != accepted || closed != 1 || status != contracts.StatusClosed {
			t.Fatalf("round %d: vote accepted=%v but ballots=%d voted=%d closed=%d status=%s", round, voteErr == nil, ballots, voted, closed, status)
		}
	}
}
