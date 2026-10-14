package httpx_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/limit"
)

func TestStreamedPostgresReplayPreservesTheAnswerAfterAConflict(t *testing.T) {
	api, router, _ := keyedSetup(t, idempotency.Postgres(httpx.ConnFrom), limit.Memory())
	runs := 0
	type output struct {
		Body struct {
			Bytes string `json:"bytes"`
		}
	}
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "create-persisted-upload", Method: http.MethodPost, Path: "/persisted-uploads",
		DefaultStatus: http.StatusCreated,
		Extensions:    map[string]any{httpx.StreamedBodyExtension: true},
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*output, error) {
		r, ok := httpx.RequestFrom(ctx)
		if !ok {
			t.Fatal("handler received no request")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		runs++
		out := &output{}
		out.Body.Bytes = string(body)
		return out, nil
	})
	const path = "/api/v1/public/probe/persisted-uploads"
	first := keyedPost(t, router, http.MethodPost, path, "persisted-upload", "original bytes")
	if first.Code != http.StatusCreated || !strings.Contains(first.Body.String(), `"bytes":"original bytes"`) {
		t.Fatalf("first upload = %d %s", first.Code, first.Body.String())
	}
	conflict := keyedPost(t, router, http.MethodPost, path, "persisted-upload", "changed bytes")
	if conflict.Code != http.StatusUnprocessableEntity || !strings.Contains(conflict.Body.String(), httpx.CodeIdempotencyConflict) {
		t.Fatalf("changed upload = %d %s, want a named conflict", conflict.Code, conflict.Body.String())
	}
	retry := keyedPost(t, router, http.MethodPost, path, "persisted-upload", "original bytes")
	if retry.Code != first.Code || retry.Body.String() != first.Body.String() || retry.Header().Get("Content-Type") != first.Header().Get("Content-Type") {
		t.Errorf("retry = %d %s, want first answer %d %s", retry.Code, retry.Body.String(), first.Code, first.Body.String())
	}
	if runs != 1 {
		t.Errorf("upload executed %d times, want 1", runs)
	}
	for i, response := range []*http.Response{first.Result(), conflict.Result(), retry.Result()} {
		response.Body.Close()
		if response.Header.Get(httpx.RateLimitPolicyHeader) == "" || response.Header.Get(httpx.RateLimitHeader) == "" {
			t.Errorf("answer %d lacks rate headers: %v", i, response.Header)
		}
		if response.Header.Get("Set-Cookie") != "" {
			t.Errorf("public answer %d sets a cookie", i)
		}
	}
}
