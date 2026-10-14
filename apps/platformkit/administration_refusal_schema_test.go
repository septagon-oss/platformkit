package main

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestAdministrationRefusalsKeepTheirPublishedPayloads(t *testing.T) {
	_, cfg := configure(t)
	body, err := app.AsyncAPI(compose(cfg).modules)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Channels map[string]struct {
			Messages map[string]struct {
				Payload struct {
					Required   []string
					Properties map[string]struct{ Type, Format string }
				}
			}
		}
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	for name, fields := range map[string]map[string]string{
		authcontracts.EventAdministrationRefused: {"role": "string", "was": "array", "now": "array", "at": "string"},
		usercontracts.EventAdministrationRefused: {"userId": "string", "attempt": "string", "roles": "array", "at": "string"},
	} {
		t.Run(name, func(t *testing.T) {
			payload := doc.Channels[name].Messages[name].Payload
			for field, kind := range fields {
				if !slices.Contains(payload.Required, field) {
					t.Errorf("%s must remain required", field)
				}
				if got := payload.Properties[field].Type; got != kind {
					t.Errorf("%s type = %q, want %q", field, got, kind)
				}
			}
			if got := payload.Properties["at"].Format; got != "date-time" {
				t.Errorf("at format = %q, want date-time", got)
			}
			if name == usercontracts.EventAdministrationRefused && payload.Properties["userId"].Format != "uuid" {
				t.Error("userId must remain a UUID")
			}
		})
	}
}
