package httpx_test

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
)

func TestStreamedPostHonorsItsIdempotencyKey(t *testing.T) {
	api, router, _ := keyedSetup(t, idempotency.Memory(nil), nil)
	runs := 0
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "create-keyed-upload", Method: http.MethodPost, Path: "/uploads",
		DefaultStatus: http.StatusCreated,
		Extensions:    map[string]any{httpx.StreamedBodyExtension: true},
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		r, ok := httpx.RequestFrom(ctx)
		if !ok {
			t.Fatal("handler received no carried request")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != "uploaded bytes" {
			t.Errorf("upload body = %q, %v", body, err)
		}
		runs++
		return &struct{}{}, nil
	})
	for range 2 {
		got := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/uploads", "upload-key", "uploaded bytes")
		if got.Code != http.StatusCreated {
			t.Fatalf("upload = %d %s", got.Code, got.Body.String())
		}
	}
	if runs != 1 {
		t.Errorf("keyed upload executed %d times, want 1", runs)
	}
}
