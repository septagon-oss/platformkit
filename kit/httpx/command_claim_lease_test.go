package httpx_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
)

// The lease is the fact behind two claims: a claim of a command still being run is
// not a candidate for the purge, and a claim nobody holds is one. Age alone says
// neither, so the row carries the renewal — which makes the row's claim about its
// owner worth testing on its own, beside the purge case that reads it.

func TestTheClaimsLeaseIsTheOneThisPackageRenews(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan *httptest.ResponseRecorder, 1)
	finish := sync.OnceFunc(func() { close(release) })
	defer finish()
	mount(t, api, "lease-note", "/lease-notes", true, func(ctx context.Context, in *noteIn) (*noteOut, error) {
		out, err := note(runs)(ctx, in)
		if err == nil {
			close(started)
			<-release
		}
		return out, err
	})
	go func() { done <- send(t, router, at(api, "/lease-notes"), keyA, "holding a lease") }()
	<-started
	// The lease is the length this package says it is, in the units Postgres reads.
	// A drift between claimLease and the interval in the INSERT — minutes written as
	// seconds, or a constant edited without the other — moves this number, and the
	// purge's whole claim about a dead owner rests on the two agreeing.
	if secs := leaseSeconds(t, f); secs < 60 || secs > 180 {
		t.Errorf("the claim's lease is %.0f seconds; want the two minutes this package renews it for", secs)
	}
	finish()
	if first := <-done; first.Code != http.StatusOK {
		t.Fatalf("owning command = %d %s; want 200", first.Code, first.Body)
	}
	// An answered claim has no owner left to renew anything: the row that says the
	// command ran must never read as a command still being answered.
	if secs := leaseSeconds(t, f); secs != -1 {
		t.Errorf("the answered claim still holds a lease of %.0f seconds; want none", secs)
	}
}

// TestThePurgeSpendsTheClaimOfACommandWhoseLeaseLapsed is the other half of the
// fence, and the shape of its recovery: a process that died mid-command stops
// renewing, and once its lease has lapsed the purge acts — by settling the claim as
// "its outcome is not held" and keeping the key, not by handing the key back. A lapsed
// lease is the fact that nobody can answer the claim, and never the fact that the
// command did not commit: the record that would have said so is written after the
// commit, detached, and can itself fail, which is
// TestACommandWhoseAnswerCannotBeStoredIsNotAppliedAgainAfterPurge. Freeing such a key
// is a permission to write twice. A cure that only ever kept unsettled rows would leave
// a dead claim holding a key forever, and it does not: the key is spent for the day the
// claim was written for, no longer than the promise in the document says (docs/adr/0016).
func TestThePurgeSpendsTheClaimOfACommandWhoseLeaseLapsed(t *testing.T) {
	_, router, f, runs, path := setupNote(t)
	if first := send(t, router, path, keyA, "abandoned"); first.Code != http.StatusOK {
		t.Fatalf("first command = %d %s; want 200", first.Code, first.Body)
	}
	// Back into the in-flight state, as the abandoned-claim case above does it, with
	// a lease that lapsed and a deadline the purge is looking for.
	runSystem(t, f, "UPDATE platformkit_idempotency SET settled = false, status = 0, response = NULL,"+
		" claimed_at = now() - interval '5 minutes 1 second', expires_at = now() - interval '1 second',"+
		" owner_lease = now() - interval '1 second'")
	if err := httpx.PurgeIdempotency(t.Context(), f.app); err != nil {
		t.Fatal(err)
	}
	// The row is what stands between this key and a second write, so the row survives
	// the job that found it, and it survives settled: a purge that left the claim
	// standing would be a purge that told nobody what it had decided, and one that
	// deleted it would be the fence deciding, on the age of a marker, that a write it
	// cannot see never happened.
	if rows, answers := spentRows(t, f); rows != 1 || answers != 1 {
		t.Fatalf("after the purge the table holds %d rows and %d answers; want the one claim the job spent", rows, answers)
	}
	repeat := send(t, router, path, keyA, "abandoned")
	if repeat.Code != http.StatusConflict || !strings.Contains(repeat.Body.String(), httpx.CodeIdempotencyResponseNotHeld) {
		t.Fatalf("after the purge the dead claim answered %d replay=%q; want the refusal that says its outcome is not held",
			repeat.Code, repeat.Header().Get("Idempotency-Replay"))
	}
	if n := runs.Load(); n != 1 {
		t.Errorf("the command ran %d times; want the one abandoned run", n)
	}
	// The day the key was written for is still its whole window, and no longer: aged
	// past it, the spent answer goes on the job's ordinary terms and the key is a fresh
	// command. A person whose command died presses again and is told to look; a person
	// whose day is over is told nothing at all.
	runSystem(t, f, "UPDATE platformkit_idempotency SET expires_at = now() - interval '1 hour' WHERE claimed_at < now() - interval '1 minute'")
	if err := httpx.PurgeIdempotency(t.Context(), f.app); err != nil {
		t.Fatal(err)
	}
	if n := heldRows(t, f, true); n != 0 {
		t.Fatalf("a spent claim past its day left %d rows; want none", n)
	}
	fresh := send(t, router, path, keyA, "abandoned")
	if fresh.Code != http.StatusOK || fresh.Header().Get("Idempotency-Replay") == "true" {
		t.Fatalf("a key past its day answered %d replay=%q; want a fresh command",
			fresh.Code, fresh.Header().Get("Idempotency-Replay"))
	}
	if n := runs.Load(); n != 2 {
		t.Errorf("the command ran %d times; want the abandoned run and this one", n)
	}
}

// spentRows is what the table holds and how much of it is an answer rather than a
// claim: the two numbers that tell one purge from another. Counting rows alone cannot
// say whether a dead claim was spent or merely left standing where it was.
func spentRows(t *testing.T, f *fixture) (rows, answers int) {
	t.Helper()
	err := db.RunSystem(t.Context(), f.app, syscap.NewSystemToken("kit/httpx test: the state of a spent claim"),
		func(_ context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Raw("SELECT count(*), count(*) FILTER (WHERE settled) FROM platformkit_idempotency").Row().Scan(&rows, &answers)
		})
	if err != nil {
		t.Fatal(err)
	}
	return rows, answers
}

// leaseSeconds is how far ahead the row's lease reaches, or -1 for a row with no
// owner attached to it any more.
func leaseSeconds(t *testing.T, f *fixture) float64 {
	t.Helper()
	var secs sql.NullFloat64
	err := db.RunSystem(t.Context(), f.app, syscap.NewSystemToken("kit/httpx test: the claim's lease"),
		func(_ context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Raw("SELECT extract(epoch from owner_lease - now()) FROM platformkit_idempotency").Row().Scan(&secs)
		})
	if err != nil {
		t.Fatal(err)
	}
	if !secs.Valid {
		return -1
	}
	return secs.Float64
}
