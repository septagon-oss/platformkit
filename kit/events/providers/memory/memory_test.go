package memory_test

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/events/internal/delivery"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

func TestIndependentMemoryCompositionsWaitForTheirOwnHandling(t *testing.T) {
	first, second := memory.New(), memory.New()
	event := transport.Event{ID: uuid.New(), Name: "notes.created", TenantID: uuid.New()}
	entered, release := make(chan struct{}), make(chan struct{})
	var other atomic.Int32
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := first.Subscribe(ctx, "notes-index", event.Name, transport.Sink{
		Handle: func(_ context.Context, got transport.Event) error {
			// The attempt count first: it is transport state, which is why it is
			// json:"-" and never reaches the wire, and this assertion is about the
			// envelope a subscriber was handed. Leaving it in would compare a
			// delivery against a publication and call the count a mutation.
			got.Attempt = 0
			if !reflect.DeepEqual(got, event) {
				t.Error("delivery changed the event envelope")
			}
			close(entered)
			<-release
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := second.Subscribe(ctx, "notes-index", event.Name, transport.Sink{
		Handle: func(context.Context, transport.Event) error { other.Add(1); return nil },
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- first.Publish(ctx, event) }()
	<-entered
	select {
	case err := <-done:
		t.Fatalf("Publish finished before its handler: %v", err)
	default:
	}
	close(release)
	if err := <-done; err != nil || other.Load() != 0 {
		t.Fatalf("independent composition received the event: count=%d err=%v", other.Load(), err)
	}
}

func TestTerminalPersistenceRetriesWithoutAnotherHandlerAttempt(t *testing.T) {
	before := delivery.Backoff
	delivery.Backoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { delivery.Backoff = before })
	var handled, recorded int
	poison := errors.New("poison event")
	provider := memory.New()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := provider.Subscribe(ctx, "notes-index", "notes.created", transport.Sink{
		Handle: func(context.Context, transport.Event) error { handled++; return poison },
		Dead: func(_ context.Context, _ transport.Event, cause error) error {
			recorded++
			if !errors.Is(cause, poison) {
				t.Error("terminal recording lost the handler's cause")
			}
			if recorded < 3 {
				return errors.New("recording unavailable")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Publish(ctx, transport.Event{Name: "notes.created"}); err != nil {
		t.Fatal(err)
	}
	if handled != 5 || recorded != 3 {
		t.Fatalf("handler=%d terminal=%d; want five handler attempts and recovered terminal recording", handled, recorded)
	}
}
