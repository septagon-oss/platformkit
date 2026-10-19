package memory_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/events/transport/observetest"
)

func TestAnObservationDoesNotHoldAPublishOpen(t *testing.T) {
	tr := memory.New()
	tenant := uuid.New()
	entered, release := make(chan struct{}), make(chan struct{})
	stop, err := tr.Observe(t.Context(), tenant, observetest.EventName, func(transport.Event) {
		close(entered)
		<-release
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	defer close(release)
	done := make(chan error, 1)
	go func() {
		done <- tr.Publish(t.Context(), transport.Event{
			ID: uuid.New(), TenantID: tenant, Name: observetest.EventName, At: time.Now().UTC(), Payload: []byte(`{}`),
		})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("observation never received the published event")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Publish waited for its ephemeral observation to return; the durable publication must make progress independently")
	}
}
