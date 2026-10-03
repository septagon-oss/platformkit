package httpx_test

// The sixth review's pins, left in the tree by a reviewer who did not write the fix.
//
// Round 9 moved the encoder of every refusal decided inside the huma chain: `refuse`
// now ends at `writeProblem` rather than at `huma.WriteErr`. Two things hold that new
// line, and before this file neither was asserted anywhere.
//
//  1. The document a guard's refusal carries is the *kernel's* shape — `type`, `title`,
//     `status`, `detail`, `instance`, and nothing else: no `"$schema"` member in the body
//     and no `Link: …; rel="describedBy"` response header. The fifth review's case
//     compares the two roads of one host, so any single shape satisfies it, including
//     Huma's; only a statement about the shape itself keeps what kit/problem's package
//     doc ("the one error shape the API returns") and the CHANGELOG promise, and keeps
//     round 9's substitution from quietly reversing itself back to two shapes of one
//     problem — the defect that case exists to catch.
//  2. The request id inside a refusal a *handler* returns is stamped by the
//     `stampRequestID` response transformer, which after round 9 writes the only bodies
//     nothing else stamps. Round 9 names this gap in its own commit body: with the
//     transformer line commented out, "a sweep of ./kit/... ./ui/...
//     ./apps/platformkit/... ./modules/... fails nothing". These are the ~25 test lines
//     it said were missing.
//
// Each case reaches its assertion through what the fixed behaviour prints — the verdict's
// status and the caller's own request id, both of which either defect leaves untouched —
// so neither can only be green while the defect stands.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
)

// round6ID is sent by the caller, so the answer has one value to be compared against.
const round6ID = "one-request-id-for-this-case"

// round6Ask is askFor with the one thing it does not send: a request id of the caller's.
func round6Ask(t *testing.T, router http.Handler, method, path, accept string) *httptest.ResponseRecorder {
	t.Helper()
	var payload io.Reader
	if method == http.MethodPost {
		payload = strings.NewReader("{}")
	}
	req := httptest.NewRequest(method, "http://"+host+path, payload)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", accept)
	req.Header.Set(httpx.RequestIDHeader, round6ID)
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestAGuardRefusalIsAnsweredWithTheKernelsOneProblemDocumentAndNoSchemaLink holds the
// shape the fifth review compared rather than the equality of two answers: the guard's
// document is the kernel's own, the one `writeProblem` writes, and not Huma's variant of
// the same value.
func TestAGuardRefusalIsAnsweredWithTheKernelsOneProblemDocumentAndNoSchemaLink(t *testing.T) {
	api, router := guardKernel(t, guardSetup{allow: false}, nil)
	asked := round6Ask(t, router, http.MethodGet, at(api, "/plans"), "application/json")
	if asked.Code != http.StatusForbidden {
		t.Fatalf("the denial answered %d, want 403: the case says nothing about a shape it cannot reach (%s)",
			asked.Code, asked.Body.String())
	}
	if got := asked.Header().Get("Link"); got != "" {
		t.Errorf("the refusal answers Link=%q: that header belongs to Huma's writer, and round 9 moved this road to writeProblem precisely so the answer carries one shape and one Content-Length (%s)",
			got, asked.Body.String())
	}
	var fields map[string]any
	if err := json.Unmarshal(asked.Body.Bytes(), &fields); err != nil {
		t.Fatalf("the refusal is not one problem document: %v (%s)", err, asked.Body.String())
	}
	if _, stamped := fields["$schema"]; stamped {
		t.Errorf(`the refusal carries "$schema" in its body: the kernel has one encoder of this shape and it does not stamp that member (%s)`, asked.Body.String())
	}
	if got := asked.Header().Get(httpx.RequestIDHeader); got != round6ID {
		t.Errorf("X-Request-Id = %q, want the caller's %q", got, round6ID)
	}
	if want := "urn:request:" + round6ID; fields["instance"] != want {
		t.Errorf(`instance = %v, want %q: the header and the body are meant to name one request`, fields["instance"], want)
	}
	if want := httpx.CodeDenied + ":"; !strings.HasPrefix(stringField(fields["detail"]), want) {
		t.Errorf("detail = %q, want the refusal to name itself with %s", stringField(fields["detail"]), want)
	}
	delete(fields, "errors") // only a validation refusal carries it
	keys := make([]string, 0, len(fields))
	for name := range fields {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	if want := []string{"detail", "instance", "status", "title", "type"}; strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("the document carries %v; kit/problem's shape is %v — one shape means the members, not only the media type %q",
			keys, want, asked.Body.String())
	}
}

// TestARefusalAHandlerReturnsCarriesTheRequestTheTransformerStamps pins the transformer
// that round 9 left asserting nothing: a body huma marshals gets its `instance` from
// `stampRequestID`, because no kernel writer passes through it.
func TestARefusalAHandlerReturnsCarriesTheRequestTheTransformerStamps(t *testing.T) {
	api, router := guardKernel(t, guardSetup{allow: true}, nil)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "round6-conflict", Method: http.MethodGet, Path: "/round6-conflict",
	}, httpx.Permission("billing:read"), func(context.Context, *struct{}) (*body, error) {
		return nil, problem.New(http.StatusConflict, "ROUND6: this row is not in that state")
	})
	asked := round6Ask(t, router, http.MethodGet, at(api, "/round6-conflict"), "application/json")
	if asked.Code != http.StatusConflict {
		t.Fatalf("the handler's own refusal answered %d, want 409: the stamp is asserted nowhere else (%s)",
			asked.Code, asked.Body.String())
	}
	if got := asked.Header().Get(httpx.RequestIDHeader); got != round6ID {
		t.Fatalf("X-Request-Id = %q, want the caller's %q: without the header there is nothing to compare the body with", got, round6ID)
	}
	var fields map[string]any
	if err := json.Unmarshal(asked.Body.Bytes(), &fields); err != nil {
		t.Fatalf("the refusal is not one problem document: %v (%s)", err, asked.Body.String())
	}
	if want := "urn:request:" + round6ID; fields["instance"] != want {
		t.Errorf(`instance = %v, want %q: stampRequestID is the only thing that puts the request id into a body huma marshals, and no test checked it fired since round 9 moved the guards off that writer (%s)`,
			fields["instance"], want, asked.Body.String())
	}
}

func stringField(v any) string {
	s, _ := v.(string)
	return s
}
