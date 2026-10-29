package httpx_test

// A refused write is quoted twice over: the person at the page reads a request id off it, and
// the machine that called reads one off the response header. Those two quotes are the same
// string only while the renderer and the document writer take the id from one place, and the
// page is written first — a renderer that stamps its own number, or one handed a refusal
// copied off an earlier request, leaves a person quoting a write nobody refused. Nothing read
// the page's quote off a server: the page cases stop at "urn:request:" being present in a
// recorder, and the exact-quote cases read a document in one. This asks one kernel and one
// real server for both shapes, so it fails where either quote stops naming this exchange.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
)

// requestQuote is the request a refusal quotes inside its own body, in either shape.
var requestQuote = regexp.MustCompile(`urn:request:[A-Za-z0-9_-]+`)

// oneRefusalQuote returns the one request id an answer quotes in its body and insists it is
// the id that answer carries in its header. A body with no quote, or with two, is a refusal
// nobody can quote back, so there is nothing left to compare.
func oneRefusalQuote(t *testing.T, shape string, answer controlPlaneAnswer) string {
	t.Helper()
	found := requestQuote.FindAllString(answer.body, -1)
	if len(found) != 1 {
		t.Fatalf("%s quotes %d request ids (%v), want the one this write carried: %s",
			shape, len(found), found, answer.body)
	}
	if want := "urn:request:" + answer.header.Get(httpx.RequestIDHeader); found[0] != want {
		t.Errorf("%s quotes %s in its body while its header answers %s=%q: the caller cannot quote the write this answer refused",
			shape, found[0], httpx.RequestIDHeader, answer.header.Get(httpx.RequestIDHeader))
	}
	return found[0]
}

// TestACrossSiteWriteRefusalQuotesTheRequestItsHeaderQuotesInBothShapes holds one refused
// write — a POST from another site — to one request id in the shape its caller asked for: the
// page quotes the id in the markup a person reads, the document in the `instance` a machine
// parses, and each of them the id its own header carries. A page and a document that name
// different writes cannot both be the record of this refusal.
func TestACrossSiteWriteRefusalQuotesTheRequestItsHeaderQuotesInBothShapes(t *testing.T) {
	api, router := setupFault(t, documentFault)
	server := httptest.NewServer(router)
	defer server.Close()

	const elsewhere = "http://elsewhere.test"
	door := at(api, "/widgets")

	shown := crossSiteWrite(t, server.URL, elsewhere, door, browserAccept)
	// Reachability, by the verdict the guard itself answers: a write that was allowed says
	// nothing about how a refusal of it is quoted.
	if shown.status != http.StatusForbidden {
		t.Fatalf("a browser's cross-site write = %d, want 403: %s", shown.status, shown.body)
	}
	if ct := shown.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("a browser's cross-site write answered %s, want the page it reads: %s", ct, shown.body)
	}
	page := oneRefusalQuote(t, "the page a person reads", shown)

	read := crossSiteWrite(t, server.URL, elsewhere, door, "application/json")
	if read.status != http.StatusForbidden {
		t.Fatalf("a machine's cross-site write = %d, want 403: %s", read.status, read.body)
	}
	if ct := read.header.Get("Content-Type"); !strings.HasPrefix(ct, problem.ContentType) {
		t.Fatalf("a machine's cross-site write answered %s, want the document it parses: %s", ct, read.body)
	}
	document := oneRefusalQuote(t, "the document a machine parses", read)

	if page != document {
		t.Errorf("the page quotes %s where the document quotes %s: one refusal recorded as two writes", page, document)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(read.body), &fields); err != nil {
		t.Fatalf("the machine's answer is not one problem document (%v): %q", err, read.body)
	}
	if fields["instance"] != document {
		t.Errorf("the document's instance = %v, want the quote its own body carries", fields["instance"])
	}
}
