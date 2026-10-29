package inferencetest

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/inference"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// C-15. The Nth ask is allowed and the N+1th refused with a RetryAfter inside the
// window, and the far end saw exactly N. A wrong adapter dials first and counts
// afterwards, so exhaustion costs money every time it is reached.
func allowanceBeforeWire(t *testing.T, fresh Legs) {
	engine, _, seen := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}})
	for range 3 {
		if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"}); err != nil {
			t.Fatalf("the third ask should still be allowed: %v", err)
		}
	}
	_, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"})
	spent, ok := errors.AsType[*inference.SpentError](err)
	if !ok || !errors.Is(err, inference.ErrSpent) {
		t.Fatalf("Chat(beyond the allowance) = %v, want a *SpentError over ErrSpent", err)
	}
	if spent.RetryAfter <= 0 || spent.RetryAfter > time.Hour {
		t.Errorf("RetryAfter %s, want inside the window", spent.RetryAfter)
	}
	// The refusal has to say what it is twice over: a number the caller can
	// wait on, and the class and status a door counts and answers without
	// reading a sentence. A refusal that arrives as an anonymous error is a 500
	// with the tenant's own allowance written on it.
	if got := inference.StatusFor(err); got != http.StatusTooManyRequests {
		t.Errorf("a spent allowance answers %d, want 429", got)
	}
	if spent.Class() != "allowance_spent" {
		t.Errorf("a spent allowance is counted as %q, want allowance_spent", spent.Class())
	}
	if got := seen.Asked(t); got != 3 {
		t.Errorf("the far end saw %d ask(s), want the 3 the allowance covered", got)
	}
}

// C-16. One tenant's spent allowance does not touch another's. The wrong adapter
// builds a key from caller data or forgets the tenant segment, which is the same
// argument kit/limit's scoping and cache's fixed-width prefix make.
func tenantAllowances(t *testing.T, fresh Legs) {
	engine, _, _ := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}})
	for range 3 {
		if _, err := engine.Chat(tenancy.WithTenant(askCtx(t), tenantA), inference.Request{Prompt: "what is this?"}); err != nil {
			t.Fatalf("tenant alpha's third ask: %v", err)
		}
	}
	if _, err := engine.Chat(tenancy.WithTenant(askCtx(t), tenantB), inference.Request{Prompt: "what is this?"}); err != nil {
		t.Errorf("tenant beta was refused after alpha spent its own allowance: %v", err)
	}
}

// C-17. An ask with no resolved tenant is refused, nothing counted and nothing
// sent. A wrong adapter counts under the nil UUID, so one tenant can lock the
// installation's one shared bucket and refuse strangers.
func requireTenant(t *testing.T, fresh Legs) {
	store := &countedCounter{}
	engine, store, seen := open(t, fresh, End{})
	_, err := engine.Chat(context.Background(), inference.Request{Prompt: "what is this?"})
	if !errors.Is(err, inference.ErrNoTenant) {
		t.Errorf("Chat(no tenant) = %v, want ErrNoTenant", err)
	}
	if got := inference.StatusFor(err); got != http.StatusInternalServerError {
		t.Errorf("an ask with no tenant answers %d; it is the caller's own bug, not a refusal of a person", got)
	}
	if store.reads != 0 {
		t.Errorf("the allowance was read %d time(s) for an ask with no tenant to charge", store.reads)
	}
	if got := seen.Asked(t); got != 0 {
		t.Errorf("the far end saw %d ask(s); a tenant-less ask must never reach a vendor", got)
	}
}

// C-18. A counter that cannot be reached refuses, and the far end sees nothing.
// The wrong adapter fails open on the error, which is the failure mode
// kit/limit/README.md exists to stop: an uncounted model call is a bill nobody
// authorised.
func unavailableCounter(t *testing.T, fresh Legs, leg Leg) {
	if leg.Uncountable == nil {
		t.Fatalf("%s names no uncountable leg; what its allowance does during an outage is a case, not a comment", leg.Name)
	}
	engine, _, seen := open(t, fresh, End{}, withCounter(leg.Uncountable(t)))
	_, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"})
	if !errors.Is(err, inference.ErrCounterUnavailable) {
		t.Errorf("Chat(counter down) = %v, want ErrCounterUnavailable", err)
	}
	// Fail closed means the door says "come back", never "here is your answer":
	// a 200 on an uncountable allowance is the outage the cap exists to refuse.
	if got := inference.StatusFor(err); got != http.StatusServiceUnavailable {
		t.Errorf("an allowance that cannot be counted answers %d, want 503", got)
	}
	if got := seen.Asked(t); got != 0 {
		t.Errorf("the far end saw %d ask(s) while the allowance could not be counted", got)
	}
}

// C-20. Every ask that reached the far end is counted, including the answers
// refused after the wire. A wrong adapter refunds a spend the provider already
// paid for, which makes a chatty endpoint free to hammer.
func countEveryAsk(t *testing.T, fresh Legs, leg Leg) {
	deviates(t, leg)
	store := &countedCounter{}
	engine, store, seen := open(t, fresh, End{Answers: []inference.Response{{Text: strings.Repeat("a cat ", 20)}}})
	for range 3 {
		if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"}); !errors.Is(err, inference.ErrAnswerTooLarge) {
			t.Fatalf("every ask should be refused for size here, and was not: %v", err)
		}
	}
	n, _, err := store.Count(askCtx(t), inference.Bucket, time.Hour)
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	if got := seen.Asked(t); n != got {
		t.Errorf("the allowance holds %d and the far end served %d; a call that cost is a call that counts", n, got)
	}
}

// C-23. N concurrent asks at a cap of 1 put one ask on the wire. The wrong
// adapter reads the same count in two goroutines, dials twice, and finds the cap
// was a suggestion. Run under -race.
func concurrentAllowance(t *testing.T, fresh Legs) {
	engine, _, _ := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}}, calls(1))
	var wg sync.WaitGroup
	allowed := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			_, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"})
			allowed <- err
		})
	}
	wg.Wait()
	close(allowed)
	sent := 0
	for err := range allowed {
		if err == nil {
			sent++
		}
	}
	if sent != 1 {
		t.Errorf("%d of 8 concurrent asks were answered, at an allowance of 1", sent)
	}
}
