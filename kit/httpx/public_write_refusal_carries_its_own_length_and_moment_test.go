// A public write limit stops a person at a form, and what they have to go on is one answer:
// the status that stopped them, the wait they are told, and a body that arrives whole. A
// recorder hands back what the writer put in it, so an answer whose length disagrees with its
// body, whose stamp came from somebody else's response, or whose Retry-After is not a number
// of seconds reads clean there. This asks one real server and reads what a client reads.
//
// refusal_shape_test.go measures the length of the problem document the limit answers with;
// the page the renderer answers with, and the moment either answer was written, are the parts
// no length check reaches, so both renderer ends are walked here.
package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
)

func TestThePublicWriteRefusalAnsweredToACallerIsOneDocumentOfItsOwnLengthAndMoment(t *testing.T) {
	const path = "/ask"

	for _, shape := range []struct {
		name   string
		answer bool
		inside string
	}{
		{"the renderer answering with a page", true, "<!doctype html>"},
		{"the renderer declining and the document answering", false, ""},
	} {
		t.Run(shape.name, func(t *testing.T) {
			fault := func(http.ResponseWriter, *http.Request, *problem.Problem) bool { return false }
			if shape.answer {
				fault = documentFault
			}
			api, router := guardKernel(t, guardSetup{allow: true, limiter: &window{allow: 0}}, fault)
			server := httptest.NewServer(router)
			defer server.Close()

			door := publicly(api, path)
			asked := controlPlaneSend(t, http.MethodPost, "{}", server.URL, host, door, browserAccept,
				"one-write-at-the-limit")
			// Reachability, by the status the limit itself answers: a write that was not
			// stopped says nothing about how a stop is written down.
			if asked.status != http.StatusTooManyRequests {
				t.Fatalf("the limit refused %s = %d, want 429: %s", door, asked.status, asked.body)
			}

			// The wait has to be a number of seconds a client can count down, not merely a
			// header present: "0" sends the caller straight back and "" sends them nowhere.
			after := asked.header.Get("Retry-After")
			if seconds, err := strconv.Atoi(after); err != nil || seconds < 1 {
				t.Errorf("the refusal says Retry-After=%q, which is no whole second to wait for (%v)", after, asked.header)
			}

			// One moment, taken while this exchange was open — the answer a cached refusal
			// would carry one from the response it was copied off.
			controlPlaneDate(t, door, asked)

			if got := asked.header.Get("Content-Length"); got != strconv.Itoa(len(asked.body)) {
				t.Errorf("%s answers Content-Length=%s in front of %d bytes of body: a reader stops where it was told to (%q)",
					door, got, len(asked.body), asked.body)
			}

			if shape.inside == "" {
				var one map[string]any
				if err := json.Unmarshal([]byte(asked.body), &one); err != nil {
					t.Errorf("the wire carries no single problem document (%v): %q", err, asked.body)
				}
			} else if n := strings.Count(asked.body, shape.inside); n != 1 {
				t.Errorf("the answer carries %d pages under one length, want one: %s", n, asked.body)
			}

			if ct := asked.header.Get("Content-Type"); asked.header.Get(httpx.RequestIDHeader) == "" {
				t.Errorf("%s refused with %s and no request id, so the caller cannot say which write was stopped", door, ct)
			}
		})
	}
}
