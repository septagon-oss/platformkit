package openai_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/septagon-oss/platformkit/kit/inference"
)

func TestStandardChatCompletionFieldsAreAccepted(t *testing.T) {
	for _, field := range []string{"control", "id", "object", "created", "index", "logprobs", "total_tokens", "complete envelope"} {
		t.Run(field, func(t *testing.T) {
			choice := map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "a cat"},
				"finish_reason": "stop",
			}
			usage := map[string]any{"prompt_tokens": 11, "completion_tokens": 4}
			body := map[string]any{"model": "vendor-model", "choices": []any{choice}, "usage": usage}
			for key, value := range map[string]any{"id": "chatcmpl-example", "object": "chat.completion", "created": 1720000000} {
				if field == key || field == "complete envelope" {
					body[key] = value
				}
			}
			if field == "index" || field == "complete envelope" {
				choice["index"] = 0
			}
			if field == "logprobs" || field == "complete envelope" {
				choice["logprobs"] = nil
			}
			if field == "total_tokens" || field == "complete envelope" {
				usage["total_tokens"] = 15
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(body); err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(server.Close)
			got, err := dial(t, server).Chat(withTenant, ask)
			if err != nil {
				t.Fatalf("standard completion with %s was refused: %v", field, err)
			}
			want := inference.Usage{InputTokens: 11, OutputTokens: 4, Reported: true}
			if got.Text != "a cat" || got.Model != "vendor-model" || got.Usage != want {
				t.Fatalf("completion = %+v; want complete text, model and reported usage", got)
			}
		})
	}
}
