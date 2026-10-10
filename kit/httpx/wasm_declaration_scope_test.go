package httpx_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// TestAWASMDeclarationWidensNoAnswerThatIsNotADocument pins the boundary the
// declaration lives behind: the policy branch in headers.go runs only for a
// response whose Content-Type says html, so a JSON answer whose handler declared
// WebAssembly — by mistake, or because one code path serves both shapes — gains
// no Content-Security-Policy at all, let alone one carrying 'wasm-unsafe-eval'.
// A declaration is a claim about a document, and an answer that is not a
// document has nothing for it to widen.
func TestAWASMDeclarationWidensNoAnswerThatIsNotADocument(t *testing.T) {
	api, router, _ := setup(t)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "read-answer", Method: http.MethodGet, Path: "/answer",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*body, error) {
		httpx.AllowWASM(ctx)
		return &body{}, nil
	})

	res := get(t, router, at(api, "/answer"))
	if res.Code != http.StatusOK {
		t.Fatalf("the answer = %d %s", res.Code, res.Body)
	}
	if csp := res.Header().Get("Content-Security-Policy"); csp != "" {
		t.Errorf("a JSON answer carries the policy %q; a declaration must widen only a document", csp)
	}
}
