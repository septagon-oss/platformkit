package httpx_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
)

func TestAKeyCannotReplayAnAnswerForDifferentEscapedPathParameters(t *testing.T) {
	api, router, _ := keyedSetup(t, idempotency.Memory(nil), nil)
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
		OperationID: "update-grouped-record", Method: http.MethodPatch, Path: "/grouped-records/{group}/{id}",
	}, httpx.Public(), func(_ context.Context, in *input) (*output, error) {
		out := &output{}
		out.Body.Group, out.Body.ID = in.Group, in.ID
		return out, nil
	})
	const firstPath = "/api/v1/public/probe/grouped-records/a%2Fb/c"
	const secondPath = "/api/v1/public/probe/grouped-records/a/b%2Fc"
	first := keyedPost(t, router, http.MethodPatch, firstPath, "escaped-resource", "{}")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"group":"a/b"`) || !strings.Contains(first.Body.String(), `"id":"c"`) {
		t.Fatalf("first resource = %d %s", first.Code, first.Body.String())
	}
	// Establish the second route's identity without depending on a replay's answer.
	control := keyedPost(t, router, http.MethodPatch, secondPath, "independent-resource", "{}")
	if control.Code != http.StatusOK || !strings.Contains(control.Body.String(), `"group":"a"`) || !strings.Contains(control.Body.String(), `"id":"b/c"`) {
		t.Fatalf("second resource under its own key = %d %s", control.Code, control.Body.String())
	}
	second := keyedPost(t, router, http.MethodPatch, secondPath, "escaped-resource", "{}")
	if second.Code == http.StatusUnprocessableEntity {
		return
	}
	if second.Code != control.Code || second.Body.String() != control.Body.String() {
		t.Errorf("second resource = %d %s; want its own answer %d %s or 422, never the first resource's answer", second.Code, second.Body.String(), control.Code, control.Body.String())
	}
}
