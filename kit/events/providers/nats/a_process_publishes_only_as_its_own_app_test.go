package nats_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	natsio "github.com/nats-io/nats.go"

	"github.com/septagon-oss/platformkit/kit/appname"
	natsprovider "github.com/septagon-oss/platformkit/kit/events/providers/nats"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

// TestAProcessPublishesOnlyAsItsOwnApp: an event that names another app is refused
// at the publish and reaches the broker at no address; an event that names none is
// stamped with the process's app and written at that app's address, its document
// naming the app.
func TestAProcessPublishesOnlyAsItsOwnApp(t *testing.T) {
	url := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if url == "" {
		t.Fatal("PLATFORMKIT_TEST_NATS_URL is unset")
	}
	collect := appname.MustParse("collect")
	tr, err := natsprovider.JetStream(collect, url)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.(interface{ Close() error }).Close()
	watch, err := natsio.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	js, err := watch.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js.AddStream(&natsio.StreamConfig{Name: streamName, Subjects: []string{appname.SubjectSpace()},
		Storage: natsio.FileStorage, Retention: natsio.LimitsPolicy}); err != nil &&
		err.Error() != "nats: stream name already in use" {
		t.Fatalf("the %s stream: %v", streamName, err)
	}
	tenant := uuid.New()
	seen := make(chan *natsio.Msg, 8)
	sub, err := watch.ChanSubscribe(appname.Prefix+".>", seen)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe()
	if err := watch.Flush(); err != nil {
		t.Fatal(err)
	}
	const name = "billing.publisher_checked"
	ev := transport.Event{ID: uuid.New(), Name: name, TenantID: tenant, At: time.Now().UTC(),
		Payload: []byte(`{"plan":"team"}`)}

	foreign := ev
	foreign.App = appname.MustParse("academy")
	if err := tr.Publish(context.Background(), foreign); err == nil || !strings.Contains(err.Error(), "academy") {
		t.Fatalf("publishing an academy event from a collect process answered %v, want a refusal naming academy", err)
	}
	if err := tr.Publish(context.Background(), ev); err != nil {
		t.Fatalf("publish an event that names no app: %v", err)
	}
	want := transport.AppSubject(collect, tenant, name)
	for {
		select {
		case msg := <-seen:
			var doc map[string]any
			if err := json.Unmarshal(msg.Data, &doc); err != nil || doc["type"] != name {
				continue
			}
			if doc["tenantid"] != tenant.String() {
				continue
			}
			if doc["id"] == foreign.ID.String() && doc["app"] == "academy" {
				t.Fatalf("the refused event reached the broker at %s", msg.Subject)
			}
			if msg.Subject != want || doc["app"] != "collect" {
				t.Fatalf("the stamped event is at %s naming app %v, want %s naming collect", msg.Subject, doc["app"], want)
			}
			return
		case <-time.After(5 * time.Second):
			t.Fatalf("the stamped event never reached %s", want)
		}
	}
}
