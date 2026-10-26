package httpx_test

// A person at a stopped form quotes the write they were refused off the page in front of
// them. One answer quoting its own request id is not enough to keep that true: a renderer
// that answers from the previous refusal, or a kernel that stops the clock on its own
// number, quotes its own answer cleanly and quotes somebody else's write to the next
// person. Two exchanges are the smallest thing that can tell those apart, and no case
// asks for two: the others stop a write once and read that one answer. This asks one
// server to stop two writes, each named by the id its caller sent, and insists each page
// names the write it stopped.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWritesThePublicLimitStopsQuoteTheirOwnWrite holds two refused writes to two
// people, each of whom is shown the write they made. A page quoting the other's id sends
// the second person to report a refusal that was never theirs.
func TestWritesThePublicLimitStopsQuoteTheirOwnWrite(t *testing.T) {
	const path = "/ask"
	api, router := guardKernel(t, guardSetup{allow: true, limiter: &window{allow: 0}}, documentFault)
	server := httptest.NewServer(router)
	defer server.Close()
	door := publicly(api, path)

	quoted := map[string]string{}
	for _, write := range []string{"the-write-at-the-front", "the-write-behind-it"} {
		stopped := controlPlaneSend(t, http.MethodPost, "{}", server.URL, host, door, browserAccept, write)
		// Reachability, by the status the limit itself answers: a write it let through
		// says nothing about how a stop it gave is written down.
		if stopped.status != http.StatusTooManyRequests {
			t.Fatalf("the limit refused %s = %d, want 429: %s", door, stopped.status, stopped.body)
		}
		if ct := stopped.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("%s answered %s to a person at a form, want the page they read: %s", door, ct, stopped.body)
		}
		found := requestQuote.FindAllString(stopped.body, -1)
		if len(found) != 1 {
			t.Fatalf("the page for %s quotes %d request ids (%v), want the one that write carried: %s",
				write, len(found), found, stopped.body)
		}
		if want := "urn:request:" + write; found[0] != want {
			t.Errorf("the page for the write sent as %q quotes %s: that person cannot say which write the limit stopped",
				write, found[0])
		}
		quoted[write] = found[0]
	}
	if quoted["the-write-at-the-front"] == quoted["the-write-behind-it"] {
		t.Errorf("two writes the limit stopped are quoted both as %s: the second person is shown the first one's refusal",
			quoted["the-write-behind-it"])
	}
}
