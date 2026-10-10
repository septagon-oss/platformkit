package limit_test

// burst_test.go is the case every other case in this package is serial without:
// N attempts at one key released together, on a store slowed by the only
// mechanism that matters here — one transaction holding that one row's lock.
//
// It exists because of a CI run. septagon-clients' flagship case fired 75
// anonymous submissions at one public door and measured "76 within the
// allowance, 0 refused with a 429" (T-0126 round 83, CI run 52994 job 53535)
// where it measured 60 and 16 on a laptop. Nothing was wrong with the count: the
// row serialises same-key attempts, so a burst is a queue, and the tail of that
// queue spent the budget kit/limit gives one attempt before it ever reached the
// row. What came back was an error, ADR 0010 tells every caller to allow the
// attempt on an error, and the callers obey — so a busy counter admitted the
// flood it exists to refuse. That is why the assertions below are on counts, and
// one of them is on what a caller that fails open would let through.
//
// The store is slowed by the real thing rather than by a Go timer: another
// session takes the row's lock and holds it, which is what a loaded runner does
// to this row. No assertion reads a stopwatch — if the queue somehow never
// forms, every attempt is counted, and 75 counted attempts at an allowance of 60
// fails the case by itself rather than skipping it.

import (
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const (
	// burstKnocks and burstAllowance are the client's own figures: 75 goroutines
	// and an allowance of 60, which is also kit/httpx's publicWriteLimit. The
	// allowance is deliberately below the number of knocks, so a run in which no
	// attempt is refused cannot pass.
	burstKnocks    = 75
	burstAllowance = 60
)

// TestABurstAtOneKeyIsAnsweredByTheLimiterRatherThanLeftWaiting is the
// acceptance case at the mechanism, with the router taken out.
func TestABurstAtOneKeyIsAnsweredByTheLimiterRatherThanLeftWaiting(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	l := limit.Postgres(httpx.ConnFrom)

	// The counter is a fixed window somebody else may have spent, so open the
	// window first: what is counted below is this case's burst. The knock that
	// opens it is counted, because it is an attempt at the same key.
	admitted := 1
	if _, _, err := l.Allow(ctx, "burst", burstAllowance, window); err != nil {
		t.Fatalf("the knock that opened the window: %v", err)
	}
	dbtest.Hold(t, admin, 10*time.Second,
		"UPDATE platformkit_limits SET count = count WHERE key = $1", storedKeys(t, admin, 1)[0])

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		errored int
		refused int
	)
	knocks := make(chan struct{})
	for shot := range burstKnocks {
		wg.Add(1)
		go func(shot int) {
			defer wg.Done()
			<-knocks
			ok, retry, err := l.Allow(ctx, "burst", burstAllowance, window)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				errored++
				t.Logf("knock %d: %v", shot+1, err)
			case !ok:
				refused++
				if retry <= 0 || retry > window {
					t.Errorf("knock %d refused with retryAfter %s, want what is left of %s", shot+1, retry, window)
				}
			default:
				admitted++
			}
		}(shot)
	}
	close(knocks)
	wg.Wait()

	// Every attempt is answered. This is the sentence that was red before the
	// fix: all 75 came back "context deadline exceeded", which is the shape a
	// store that has vanished comes back in, and which every composer of this
	// package is told to answer by allowing the attempt.
	if errored != 0 {
		t.Errorf("%d of %d attempts were answered with an error, want none: an error is what a "+
			"down store answers with, and a caller that fails open on an error admits all of them",
			errored, burstKnocks)
	}
	if admitted > burstAllowance {
		t.Errorf("%d of %d attempts were counted at an allowance of %d, want no more than the allowance",
			admitted, burstKnocks, burstAllowance)
	}
	if refused == 0 {
		t.Errorf("none of the %d attempts was refused; the queue never formed, so this case measured nothing", burstKnocks)
	}

	// The count a fail-open composer would answer with: an attempt that errored
	// is a request it let through. This is the client's failing line — 76
	// admitted at an allowance of 60 — and it is why the refusal of a queued
	// attempt travels as ok=false rather than as an error.
	if through := admitted + errored; through > burstAllowance {
		t.Errorf("a caller that fails open on an error lets %d through at an allowance of %d", through, burstAllowance)
	}

	// And what the row holds is what it allowed. Of C recorded attempts the ones
	// at or under the allowance were allowed and the rest were refused at the row,
	// so admitted is min(C, allowance) — a refusal for queueing that also recorded
	// itself would make the limit a function of the queue instead.
	if got := storedCount(t, admin); admitted != min(got, burstAllowance) {
		t.Errorf("the row counts %d, which allows %d attempts; %d were allowed",
			got, min(got, burstAllowance), admitted)
	}
}
