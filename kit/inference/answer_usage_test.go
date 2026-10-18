package inference_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/inference"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestEverySuccessfulAnswerCarriesReportedTokenUsage(t *testing.T) {
	for _, reported := range []bool{true, false} {
		name := "reported usage"
		if !reported {
			name = "missing usage"
		}
		t.Run(name, func(t *testing.T) {
			usage := inference.Usage{}
			if reported {
				usage = inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}
			}
			provider := inference.NewScript(inference.Reply{Text: "a cat", Usage: usage})
			engine, err := inference.New(inference.Config{Model: "test-model", DailyCalls: 1, Counter: limit.Memory()}, provider)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = engine.Close() })
			got, err := engine.Chat(tenancy.WithTenant(t.Context(), tenant), inference.Request{Prompt: "what is this?"})
			if reported && (err != nil || got.Text != "a cat" || got.Usage != usage) {
				t.Errorf("reported answer = %+v, %v; want whole answer and its token usage", got, err)
			}
			if !reported && (err == nil || got != (inference.Answer{})) {
				t.Errorf("unaccounted answer = %+v, %v; an answer without token usage must be refused", got, err)
			}
		})
	}
}
