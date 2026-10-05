package httpx_test

import (
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestAnAnonymousCommandCannotSilentlyIgnoreItsDeclaredKey(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	f.principal = nil
	op := huma.Operation{OperationID: "anonymous-note", Method: http.MethodPost, Path: "/anonymous-note"}
	httpx.DeclareIdempotency(&op)
	httpx.Register(api.Surfaces(probe).App, op, httpx.Public(), note(runs))
	if err := api.ValidateDeclarations(); err != nil {
		// Rejecting an impossible declaration before listening is also safe.
		t.Logf("anonymous keyed command refused at boot: %v", err)
		return
	}
	path := at(api, "/anonymous-note")
	for range 2 {
		res := send(t, router, path, keyA, "one anonymous submission")
		if res.Code < http.StatusBadRequest {
			t.Errorf("command with no principal ran unclaimed: %d %s; want a refusal", res.Code, res.Body)
		}
	}
	if n := countRows(t, f, "notes"); n != 0 {
		t.Errorf("anonymous keyed command committed %d notes; want none", n)
	}
	if n := runs.Load(); n != 0 {
		t.Errorf("key was silently ignored and handler ran %d times", n)
	}
}
