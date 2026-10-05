package nats_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	natsio "github.com/nats-io/nats.go"

	"github.com/septagon-oss/platformkit/kit/appname"
	natsprovider "github.com/septagon-oss/platformkit/kit/events/providers/nats"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

// Each app uses the public provider constructor and the same event name on one
// broker. The provider must route the four published IDs only to their owner.
func TestTwoAppProvidersKeepTheirDeliveriesApart(t *testing.T) {
	url := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if url == "" {
		t.Fatal("PLATFORMKIT_TEST_NATS_URL is unset")
	}
	admin, err := natsio.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	js, err := admin.JetStream()
	if err != nil {
		t.Fatal(err)
	}

	const eventName = "billing.provider_routed"
	const module = "billing"
	apps := []appname.Name{appname.MustParse("acme"), appname.MustParse("acme-billing")}
	for _, app := range apps {
		durable := appname.Durable(app, module, eventName)
		if err := js.DeleteConsumer(sharedStream, durable); err != nil && !errors.Is(err, natsio.ErrConsumerNotFound) && !errors.Is(err, natsio.ErrStreamNotFound) {
			t.Fatalf("clear %s: %v", durable, err)
		}
		defer func() { _ = js.DeleteConsumer(sharedStream, durable) }()
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	run := uuid.NewString()
	type receipt struct {
		app appname.Name
		id  uuid.UUID
	}
	arrived := make(chan receipt, 16)
	providers := make([]transport.Transport, 0, len(apps))
	for _, app := range apps {
		provider, err := natsprovider.JetStream(app, url)
		if err != nil {
			t.Fatalf("connect app %s: %v", app, err)
		}
		defer provider.(interface{ Close() error }).Close()
		providers = append(providers, provider)
		if err := provider.Subscribe(ctx, appname.Durable(app, module, eventName), eventName, transport.Sink{
			Handle: func(_ context.Context, ev transport.Event) error {
				var body struct {
					Run string `json:"run"`
				}
				if json.Unmarshal(ev.Payload, &body) == nil && body.Run == run {
					arrived <- receipt{app: app, id: ev.ID}
				}
				return nil
			},
		}); err != nil {
			t.Fatalf("subscribe app %s: %v", app, err)
		}
	}

	want := make(map[uuid.UUID]appname.Name)
	for i, app := range apps {
		for range 2 {
			body, err := json.Marshal(map[string]string{"run": run, "owner": string(app)})
			if err != nil {
				t.Fatal(err)
			}
			ev := transport.Event{
				ID: uuid.New(), Name: eventName, TenantID: uuid.New(),
				At: time.Now().UTC(), Payload: body,
			}
			want[ev.ID] = app
			if err := providers[i].Publish(ctx, ev); err != nil {
				t.Fatalf("publish app %s: %v", app, err)
			}
		}
	}
	for range len(want) {
		select {
		case got := <-arrived:
			owner, ok := want[got.id]
			if !ok {
				t.Errorf("unexpected or repeated delivery %s to %s", got.id, got.app)
				continue
			}
			delete(want, got.id)
			if got.app != owner {
				t.Errorf("event %s of app %s reached app %s", got.id, owner, got.app)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%d events never reached their app", len(want))
		}
	}
}
