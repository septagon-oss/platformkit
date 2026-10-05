package httpx_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
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

// TestThePurgeTakesTheClaimOfACommandWhoseLeaseLapsed is the other half of the
// fence, and the recovery bound in a number: a process that died mid-command stops
// renewing, and once its lease has lapsed the purge is allowed to free the key. A
// cure that only ever kept unsettled rows would pass the live-owner case and leave
// a dead one holding a key forever.
func TestThePurgeTakesTheClaimOfACommandWhoseLeaseLapsed(t *testing.T) {
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
	repeat := send(t, router, path, keyA, "abandoned")
	if repeat.Code != http.StatusOK || repeat.Header().Get("Idempotency-Replay") == "true" {
		t.Fatalf("after the purge the dead claim answered %d replay=%q; want a fresh command",
			repeat.Code, repeat.Header().Get("Idempotency-Replay"))
	}
	if n := runs.Load(); n != 2 {
		t.Errorf("the command ran %d times; want the abandoned run and this one", n)
	}
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
