package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

func TestNotificationEventsCanBeSubscribedToThroughTheApplication(t *testing.T) {
	cfg, mods, fixture := deviceComposition(t)
	install(t, fixture.path)
	start(t, cfg, mods, fixture.opts)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	for _, event := range []string{
		notificationcontracts.EventPushRequested, notificationcontracts.EventWebPushRequested,
		notificationcontracts.EventWebhookRequested, notificationcontracts.EventPreferenceSet,
		notificationcontracts.EventSenderSet, notificationcontracts.EventSenderVerified,
		notificationcontracts.EventSenderRemoved,
	} {
		t.Run(event, func(t *testing.T) {
			code, body := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/webhook/endpoints",
				`{"name":"Notice receiver","url":"https://hooks.example.test/notices","events":["`+event+`"]}`)
			if code != http.StatusCreated {
				t.Fatalf("subscription for %s = %d %s, want 201", event, code, body)
			}
			id := createdRowID(t, body, "endpoint")
			code, body = do(t, cfg, admin, http.MethodGet, acmeHost, "/api/v1/webhook/endpoints/"+id, "")
			if code != http.StatusOK {
				t.Fatalf("saved subscription = %d %s, want 200", code, body)
			}
			var saved struct {
				ID     string   `json:"id"`
				Events []string `json:"events"`
			}
			if err := json.Unmarshal([]byte(body), &saved); err != nil {
				t.Fatal(err)
			}
			if saved.ID != id || !slices.Equal(saved.Events, []string{event}) {
				t.Fatalf("saved subscription = %+v, want id %s and only %s", saved, id, event)
			}
		})
	}
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/webhook/endpoints",
		`{"name":"Unknown receiver","url":"https://hooks.example.test/notices","events":["notification.never_emitted"]}`)
	if code < 400 || code >= 500 {
		t.Fatalf("unknown event subscription = %d %s, want a client refusal", code, body)
	}
}
