package main

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
)

func TestWebhookRegistrationAcceptsMailEventsAndReturnsACreatedSecret(t *testing.T) {
	cfg, mods, fixture := deviceComposition(t)
	install(t, fixture.path)
	start(t, cfg, mods, fixture.opts)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	for _, event := range []string{"auth.no_link_requested", "auth.mail_failed"} {
		t.Run(event, func(t *testing.T) {
			code, body := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/webhook/endpoints",
				`{"name":"Mail receiver","url":"https://hooks.example.test/mail","events":["`+event+`"]}`)
			if code != http.StatusCreated {
				t.Fatalf("registration = %d %s, want 201", code, body)
			}
			secret := field(t, body, "secret")
			if !strings.HasPrefix(secret, "whsec_") {
				t.Fatal("registration returned no Standard Webhooks secret")
			}
			key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
			if err != nil || len(key) != 32 {
				t.Fatalf("registration secret is not a 32-byte key: %v", err)
			}
			id := createdRowID(t, body, "endpoint")
			code, body = do(t, cfg, admin, http.MethodGet, acmeHost, "/api/v1/webhook/endpoints/"+id, "")
			if code != http.StatusOK || field(t, body, "id") != id {
				t.Fatalf("created endpoint read = %d %s", code, body)
			}
			if strings.Contains(body, secret) || strings.Contains(body, `"secret"`) {
				t.Fatal("endpoint read exposes the shown-once secret")
			}
		})
	}
}
