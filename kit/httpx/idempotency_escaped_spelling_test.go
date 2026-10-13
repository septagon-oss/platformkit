package httpx_test

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
)

// TestOneResourceSpelledTwoWaysIsRefusedRatherThanReplayed is the half of the request
// identity that the cure costs. One resource, `/records/a%2Fb/c` and `/records/a%2fb/c`,
// spelled two ways: the guard digests the address as it arrived, so those are two writes
// and the second is refused rather than answered from the first's record. The refusal is
// the cheap direction — refusing a write that found no matching record costs a caller a
// retry under a new key, while serving one caller's answer to the other's spelled write
// costs them a write they believe happened — and the document says the same thing on the
// wire: "A retry with byte-identical request and body gets the answer the first attempt
// was given" (keyDescription).
func TestOneResourceSpelledTwoWaysIsRefusedRatherThanReplayed(t *testing.T) {
	api, router, _ := keyedSetup(t, idempotency.Memory(nil), nil)
	var ran atomic.Int32
	type input struct {
		Group string `path:"group"`
		ID    string `path:"id"`
	}
	type output struct {
		Body struct {
			Group string `json:"group"`
			ID    string `json:"id"`
		}
	}
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "echo-grouped-record", Method: http.MethodPatch, Path: "/records/{group}/{id}",
	}, httpx.Public(), func(_ context.Context, in *input) (*output, error) {
		ran.Add(1)
		out := &output{}
		out.Body.Group, out.Body.ID = in.Group, in.ID
		return out, nil
	})
	first := keyedPost(t, router, http.MethodPatch, "/api/v1/public/probe/records/a%2Fb/c", "spelling", "{}")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"group":"a/b"`) {
		t.Fatalf("first write = %d %s", first.Code, first.Body.String())
	}
	second := keyedPost(t, router, http.MethodPatch, "/api/v1/public/probe/records/a%2fb/c", "spelling", "{}")
	if second.Code != http.StatusUnprocessableEntity {
		t.Errorf("the second spelling = %d %s, want 422 and never the first spelling's answer", second.Code, second.Body.String())
	}
	if ran.Load() != 1 {
		t.Errorf("%d handler calls for two spellings of one key: the first spelling's write is the only one, "+
			"and a refusal that ran the second is not a refusal", ran.Load())
	}
}
