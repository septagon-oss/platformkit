package inferencetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/inference"
)

// C-11. A deadline in the context ends the call inside itself. A wrong adapter
// keeps a goroutine and a connection after the caller gave up, which is a leak
// with a tenant's name on it.
func deadline(t *testing.T, fresh Legs, leg Leg) {
	deviates(t, leg)
	engine, _, _ := open(t, fresh, End{Silence: 5 * time.Second})
	when, cancel := context.WithTimeout(askCtx(t), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := engine.Chat(when, inference.Request{Prompt: "what is this?"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Chat(deadline) = %v, want context.DeadlineExceeded", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("the call returned %s after its deadline; a caller that gave up should not be kept", time.Since(start))
	}
}

// C-12. Cancelling mid-call returns promptly, and the allowance stays spent:
// the far end served the call. A wrong adapter refunds a spend the provider
// already paid for, or ignores its cancellation until the answer lands.
func cancelledCall(t *testing.T, fresh Legs, leg Leg) {
	deviates(t, leg)
	store := &countedCounter{}
	engine, store, _ := open(t, fresh, End{Silence: 5 * time.Second})
	givingUp, cancel := context.WithCancel(askCtx(t))
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	if _, err := engine.Chat(givingUp, inference.Request{Prompt: "what is this?"}); !errors.Is(err, context.Canceled) {
		t.Errorf("Chat(cancel) = %v, want context.Canceled", err)
	}
	cancel()
	n, _, err := store.Count(askCtx(t), inference.Bucket, time.Hour)
	if err != nil {
		t.Fatalf("counting what the cancelled call spent: %v", err)
	}
	if n != 1 {
		t.Errorf("the allowance holds %d, want 1: the far end served that call, whoever asked it to stop", n)
	}
}

// C-21. Model names the configured model, and an engine with no endpoint names
// practice. A wrong application cannot tell a real model from the fake, which is
// "it never pretends" broken.
func modelName(t *testing.T, fresh Legs) {
	engine, _, configured := open(t, fresh, End{})
	if engine.Model() != configured.Model {
		t.Errorf("Model() = %q, want the configured model", engine.Model())
	}
}

func practiceName(t *testing.T) {
	if inference.Practice().Model() != inference.PracticeModel {
		t.Errorf("Practice().Model() = %q, want %q", inference.Practice().Model(), inference.PracticeModel)
	}
}

// C-22. After Close, Chat answers ErrClosed and the far end sees nothing; the
// second Close answers ErrClosed. A wrong adapter leaves an idle pool behind and
// keeps spending an allowance nobody is asking about.
func closedEngine(t *testing.T, fresh Legs) {
	engine, _, seen := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}})
	if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"}); err != nil {
		t.Fatalf("the ask before Close: %v", err)
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"}); !errors.Is(err, inference.ErrClosed) {
		t.Errorf("Chat after Close = %v, want ErrClosed", err)
	}
	if err := engine.Close(); !errors.Is(err, inference.ErrClosed) {
		t.Errorf("the second Close = %v, want ErrClosed", err)
	}
	if got := seen.Asked(t); got != 1 {
		t.Errorf("the far end saw %d ask(s), want the one before Close", got)
	}
}
