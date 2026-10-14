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

func TestAKeyCannotReplayAnotherResourcesAnswer(t *testing.T) {
	api, router, _ := keyedSetup(t, idempotency.Memory(nil), nil)
	type input struct {
		ID   string `path:"id"`
		Body struct {
			Name string `json:"name"`
		}
	}
	type output struct {
		Body struct {
			ID string `json:"id"`
		}
	}
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "update-keyed-resource", Method: http.MethodPatch, Path: "/records/{id}",
		DefaultStatus: http.StatusOK,
	}, httpx.Public(), func(_ context.Context, in *input) (*output, error) {
		out := &output{}
		out.Body.ID = in.ID
		return out, nil
	})
	first := keyedPost(t, router, http.MethodPatch, "/api/v1/public/probe/records/first", "resource-key", `{"name":"same"}`)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"id":"first"`) {
		t.Fatalf("first write = %d %s", first.Code, first.Body.String())
	}
	second := keyedPost(t, router, http.MethodPatch, "/api/v1/public/probe/records/second", "resource-key", `{"name":"same"}`)
	// Either a distinct resource scope or a conflicting fingerprint is safe.
	if second.Code == http.StatusUnprocessableEntity {
		return
	}
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"id":"second"`) {
		t.Errorf("second resource = %d %s; want its own answer or 422, never another resource's answer", second.Code, second.Body.String())
	}
}
