package httpx_test

// The pointer to another surface's write door is a direction, and a direction is
// only worth giving to somebody who can walk it. The second review found the host
// asked and the caller not asked; these are the four corners of the one decision,
// including the two that already worked, so that the fix which stopped naming the
// door to a stranger cannot quietly stop naming it to the caller the refusal exists
// for either.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// theSplit is the arrangement the workspace catalog had to start describing: one
// resource, read where the tenant is and written where the installation is.
const (
	splitRead  = "/api/v1/billing/plans"
	splitWrite = "/api/v1/ops/billing/plans"
)

// splitResource mounts the read door and records where the writes of that resource
// answer, which is the pair a derived client reads as one ordinary resource.
func splitResource(t *testing.T) *surfaces {
	t.Helper()
	s := newSurfaces(t)
	s.api.RegisterResource(httpx.Resource{
		Module: "billing", Entity: "plan",
		Schema:    entity.Schema{Module: "billing", Entity: "plan", Path: splitRead},
		WritePath: splitWrite,
	})
	httpx.Register(s.api.Surfaces("billing").App, huma.Operation{
		OperationID: "split-read-plans", Method: http.MethodGet, Path: "/plans",
	}, httpx.Permission("billing:read"), ok)
	if err := s.api.ValidateDeclarations(); err != nil {
		t.Fatalf("the routes do not declare themselves: %v", err)
	}
	return s
}

// writeToReadDoor posts a write of the split resource to the address that reads it,
// as either a caller holding this installation's session cookie or a caller holding
// nothing. The credential is the whole difference between the two requests, which
// is what the case is about.
func writeToReadDoor(t *testing.T, s *surfaces, authority string, presentsSession bool) (int, string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "http://"+authority+splitRead, strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	if presentsSession {
		r.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	}
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

// TestTheWriteDoorOfAnotherSurfaceIsNamedToTheCallerWhoCanReachIt holds the pointer
// to the caller it is a direction for and to nobody else. The door sits on the
// control plane, which reads a session and asks for an operator grant, so the
// anonymous caller who is refused it is refused the answer the host gives anybody at
// an address that does not take the verb — and the caller who holds the session is
// not left to discover that by guessing which of this host's addresses writes.
func TestTheWriteDoorOfAnotherSurfaceIsNamedToTheCallerWhoCanReachIt(t *testing.T) {
	s := splitResource(t)

	// Reachability, with none of the answer's own words in it: the read door is
	// served at both hosts, and refuses the verb rather than the address.
	for _, authority := range []string{host, installationHost} {
		if code, body := writeToReadDoor(t, s, authority, true); code == http.StatusNotFound {
			t.Fatalf("a recognised write at %s's read door = 404; this case is about a door that is served: %s", authority, body)
		}
	}

	// The caller the refusal exists for: recognised, at the host that serves the
	// control plane. The write is still refused, and it names the way out.
	if code, body := writeToReadDoor(t, s, installationHost, true); code != http.StatusForbidden ||
		!strings.Contains(body, httpx.CodeWriteElsewhere) || !strings.Contains(body, splitWrite) {
		t.Errorf("a recognised POST at the read door of the installation host = %d %s, want %s and the address %s",
			code, body, httpx.CodeWriteElsewhere, splitWrite)
	}

	// The same caller at the same address with nothing to be recognised by. The door
	// the pointer names would refuse them the moment they arrived at it, so they get
	// the answer this host gives an anonymous caller everywhere else.
	if code, body := writeToReadDoor(t, s, installationHost, false); strings.Contains(body, "/api/v1/ops/") ||
		code != http.StatusMethodNotAllowed {
		t.Errorf("an anonymous POST at the read door of the installation host = %d %s; the pointer is a direction "+
			"for a caller who holds the credential that door reads, and this one holds nothing, so the answer has to "+
			"be the one this host gives an unrecognised caller at any other address", code, body)
	}

	// And the host that serves no control plane, whoever is standing in front of it:
	// the answer a caller gets about that surface is the same at every host that does
	// not serve it, which is what the host half of the gate exists to keep.
	if code, body := writeToReadDoor(t, s, host, true); strings.Contains(body, "/api/v1/ops/") ||
		code != http.StatusMethodNotAllowed {
		t.Errorf("a recognised POST at the read door of a host that serves no control plane = %d %s, want the "+
			"answer every other unmounted address of this host gets and no word of the surface", code, body)
	}
}
