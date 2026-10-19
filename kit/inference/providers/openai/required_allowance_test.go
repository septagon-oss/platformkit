package openai_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/septagon-oss/platformkit/kit/inference"
	"github.com/septagon-oss/platformkit/kit/inference/providers/openai"
)

func TestARealEndpointRequiresATenantAllowance(t *testing.T) {
	var sent atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"test-model","choices":[{"message":{"content":"a cat"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3}}`)
	}))
	t.Cleanup(server.Close)
	cfg := inference.Config{Model: "test-model"}
	provider, err := openai.New(cfg, openai.Config{BaseURL: server.URL, Insecure: true})
	if err != nil {
		return
	}
	t.Cleanup(func() { _ = provider.Close() })
	engine, err := inference.New(cfg, provider)
	if err != nil {
		return
	}
	t.Cleanup(func() { _ = engine.Close() })
	t.Error("construction accepted a real endpoint with no tenant allowance")
	for range 3 {
		if _, err := engine.Chat(withTenant, ask); err != nil {
			t.Logf("uncapped call was refused: %v", err)
			return
		}
	}
	t.Fatalf("a real endpoint accepted %d calls with DailyCalls=0 and Counter=nil; only Practice may be uncapped", sent.Load())
}
