package httpx_test

// A refusal comparison that runs against a real server reads what a real server writes, and
// one of the things it writes is the second it wrote the answer in. Read as bytes, that stamp
// made the pair in review_round5_refusal_shape_test.go red or green on the clock rather than
// on the tree: `make check-race` at ca9fdb5 on 2026-10-09 (run 55756, job 56412) refused the
// head because the control plane answered `Date: Fri, 09 Oct 2026 04:38:34 GMT` and the
// address nobody mounted answered `… 04:38:35 GMT`, and the same bytes passed that same step
// on the run before and the run after. A gate that answers differently about an unchanged tree
// is what this program's CI cannot afford — the task that owns this file is named for it.
//
// So the straddle is taken here on purpose, once. The pair is asked on either side of a second
// boundary, the two stamps are shown to be different seconds (the case is worthless if they
// are not, so it says so), and the refusal comparison has to hold anyway. What it holds is the
// claim README.md, CHANGELOG.md and authorize.go's notHere make — nothing a caller reads out
// of the answer says which of the two surfaces answered — and a claim of that shape cannot
// depend on when somebody happened to ask. One second of suite wall clock, paid by the only
// case that can collect it.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/problem"
)

// waitPastTheSecondBoundary sleeps to the next whole second and a little past it, so that the
// answer taken afterwards carries a Date one second later than an answer taken before the call.
// HTTP dates carry whole seconds (RFC 9110 §5.6.7), which makes the boundary a real one rather
// than a matter of nanoseconds, and the margin is what keeps a slow scheduler from landing the
// next request back inside the second it was asked in.
func waitPastTheSecondBoundary(t *testing.T) {
	t.Helper()
	next := time.Now().Truncate(time.Second).Add(time.Second)
	time.Sleep(time.Until(next) + 50*time.Millisecond)
}

func TestTwoRefusalsOfOneHostAskedAcrossASecondBoundaryAnswerAlike(t *testing.T) {
	const id = "one-request-id-for-both"
	const nowhere = "/nothing-is-mounted-at-this-address"

	declining := func(http.ResponseWriter, *http.Request, *problem.Problem) bool { return false }
	router, opsAt, appAt := round5Kernel(t, false, declining)
	server := httptest.NewServer(router)
	defer server.Close()

	controlPlane := round5Ask(t, server.URL, installationHost, opsAt("/all"), "application/json", id)
	if controlPlane.status != http.StatusNotFound {
		t.Fatalf("the control plane asked by a tenant that is not the installation's = %d %s; this case compares the 404, so it has to be one",
			controlPlane.status, controlPlane.body)
	}

	waitPastTheSecondBoundary(t)

	neverMounted := round5Ask(t, server.URL, installationHost, appAt(nowhere), "application/json", id)
	first, second := controlPlane.header.Get("Date"), neverMounted.header.Get("Date")
	if first == second {
		t.Fatalf("both answers carry Date=%s, so the pair never straddled a second boundary and this case compared nothing the other run does not already compare", first)
	}
	t.Logf("the pair straddled the boundary: %s then %s", first, second)

	round5Same(t, controlPlane, neverMounted, "the control plane at the installation host", "an address nobody mounted")
}
