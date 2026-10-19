package stream

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	natsprovider "github.com/septagon-oss/platformkit/kit/events/providers/nats"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/events/transport/observetest"
	"github.com/septagon-oss/platformkit/kit/module"
)

// TestASharedWatchOutlivesTheConnectionThatOpenedIt runs the hub over the broker
// transport a deployment ships. One (tenant, event) watch is shared by every
// connection to it; when the connection that happened to open it leaves, the
// connections still holding it must go on receiving notices.
func TestASharedWatchOutlivesTheConnectionThatOpenedIt(t *testing.T) {
	url := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if url == "" {
		t.Fatal("PLATFORMKIT_TEST_NATS_URL is unset: the transport this repository ships is not being tested")
	}
	app := appname.MustParse("observation")
	tr, err := natsprovider.JetStream(app, url)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	defer func() { _ = tr.(io.Closer).Close() }()

	f := newFixture(t, nil, slowHeartbeat())
	f.hub = New(Deps{
		Transport: tr,
		Registry: NewRegistry([]module.Module{{Name: "observetest", Events: []string{observetest.EventName},
			Streams: []module.Stream{{Event: observetest.EventName, Policy: module.StreamAnyPrincipal()}}}}),
		Authority: f.authority,
		Limits:    slowHeartbeat(),
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	publish := func() uuid.UUID {
		ev := transport.Event{ID: uuid.New(), Name: observetest.EventName, App: app, TenantID: f.tenants[0].ID,
			Payload: json.RawMessage(`{}`), At: time.Now().UTC()}
		if err := tr.Publish(context.Background(), ev); err != nil {
			t.Fatalf("publish: %v", err)
		}
		return ev.ID
	}

	first := f.open(t, f.request(0, 0, observetest.EventName))
	second := f.open(t, f.request(0, 1, observetest.EventName))
	defer second.close(t)

	// Both connections hear a publish while both hold the watch.
	both := publish()
	first.await(t, "id: "+both.String())
	second.await(t, "id: "+both.String())

	first.close(t)
	after := publish()
	if !until(func() bool { return strings.Contains(second.w.written(), "id: "+after.String()) }) {
		t.Errorf("the connection still holding the watch received nothing after the one that opened it left:\n%s",
			second.w.written())
	}
}
