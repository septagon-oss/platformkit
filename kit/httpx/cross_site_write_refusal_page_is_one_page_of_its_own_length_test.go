package httpx_test

// A page is the only thing a person at a refused write can read, and a recorder hands back
// whatever the writer put in it: a page longer than its own Content-Length, a stamp copied
// off somebody else's answer, or an answer with no request id to quote all read clean
// there. The two cases that measure a refusal's length today ask it of the problem document
// (refusal_shape_test.go) and of the limit that stops an anonymous form post
// (public_write_refusal_carries_its_own_length_and_moment_test.go); the refusal a browser
// reaches most often — a write that came from another origin — has only ever been read off
// a recorder, so this asks it of one real server and reads what the window reads.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// crossSiteWrite is one browser's write from `origin`, read off a real server the way
// controlPlaneSend reads one, so both cases judge one answer rather than two shapes of it.
// The Origin is the whole of what makes this refusal: the guard that answers it runs before
// any handler, which is why the page has never been the handler's to write.
func crossSiteWrite(t *testing.T, server, origin, path, accept string) controlPlaneAnswer {
	t.Helper()
	sent := time.Now()
	req, err := http.NewRequest(http.MethodPost, server+path, strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	req.Host = host
	req.Header.Set("Accept", accept)
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpx.RequestIDHeader, "a-write-from-another-site")
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	defer res.Body.Close()
	read, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return controlPlaneAnswer{status: res.StatusCode, header: res.Header, body: string(read),
		sentAt: sent, readAt: time.Now()}
}

// TestACrossSiteWriteRefusalAnsweredWithAPageIsOnePageOfTheLengthItPromises holds the page
// a browser is given to the promise its own headers make: one page, of the length the answer
// says it is, stamped at the moment it was written, naming the write it refused. It judges
// the answer a client reads, so a kernel that writes the page whole and says so passes, and
// one that promises a different number of bytes than it sends or shows a stale stamp fails.
func TestACrossSiteWriteRefusalAnsweredWithAPageIsOnePageOfTheLengthItPromises(t *testing.T) {
	api, router := setupFault(t, documentFault)
	server := httptest.NewServer(router)
	defer server.Close()

	door := at(api, "/widgets")
	asked := crossSiteWrite(t, server.URL, "http://elsewhere.test", door, browserAccept)
	// Reachability, by the verdict the guard itself answers: a write that was allowed says
	// nothing about how a refusal of it is written down.
	if asked.status != http.StatusForbidden {
		t.Fatalf("the origin guard answered %s = %d, want 403: %s", door, asked.status, asked.body)
	}
	if ct := asked.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("%s answered %s to a browser, want a page it can read: %s", door, ct, asked.body)
	}
	if got := asked.header.Get("Content-Length"); got != strconv.Itoa(len(asked.body)) {
		t.Errorf("%s answers Content-Length=%s in front of %d bytes of body: the person stops reading where they were told to (%q)",
			door, got, len(asked.body), asked.body)
	}
	if n := strings.Count(asked.body, "<!doctype html>"); n != 1 {
		t.Errorf("the answer carries %d pages under one length, want the one this person was shown: %s", n, asked.body)
	}
	// One moment, taken while this exchange was open: a page copied off an earlier answer
	// carries that answer's stamp, which is when somebody else was refused.
	controlPlaneDate(t, door, asked)
	if asked.header.Get(httpx.RequestIDHeader) == "" {
		t.Errorf("%s refused with no request id, so the person at the page cannot say which write was refused", door)
	}
}
