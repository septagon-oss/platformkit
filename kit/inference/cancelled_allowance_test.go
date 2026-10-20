package inference_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/inference"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAnEndedContextSpendsNoInferenceAllowance(t *testing.T) {
	for _, ended := range []string{"cancelled", "expired deadline"} {
		t.Run(ended, func(t *testing.T) {
			ctx := tenancy.WithTenant(t.Context(), tenant)
			store := limit.Memory()
			provider := inference.NewScript(inference.Reply{Text: "a cat", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}})
			engine, err := inference.New(inference.Config{Model: "test-model", DailyCalls: 1, Counter: store}, provider)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = engine.Close() })
			stopped, cancel := context.WithCancel(ctx)
			wantErr := context.Canceled
			if ended == "expired deadline" {
				cancel()
				stopped, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				wantErr = context.DeadlineExceeded
			}
			cancel()
			got, err := engine.Chat(stopped, inference.Request{Prompt: "what is this?"})
			if !errors.Is(err, wantErr) || got != (inference.Answer{}) {
				t.Errorf("Chat with ended context = %+v, %v; want no answer and %v", got, err, wantErr)
			}
			n, _, err := store.Count(ctx, inference.Bucket, 24*time.Hour)
			if err != nil || n != 0 {
				t.Errorf("ended context spent %d calls, error %v; want zero because no provider call was possible", n, err)
			}
			if len(provider.Asks()) != 0 {
				t.Errorf("an ended context reached the provider %d times", len(provider.Asks()))
			}
			if _, err := engine.Chat(ctx, inference.Request{Prompt: "what is this?"}); err != nil {
				t.Errorf("the next live call lost its allowance: %v", err)
			}
		})
	}
}
