package site_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestDefaultSiteColourCanBeSavedUnchanged holds the read and the write door
// against each other: whatever a tenant is shown as its own colour is a value the
// same door accepts back, so a settings form that puts what it was given changes
// nothing rather than refusing its own default. The row behind this answer holds
// the absence of a colour; the answer is the installation's accent, which is a
// #rrggbb the published pattern admits.
func TestDefaultSiteColourCanBeSavedUnchanged(t *testing.T) {
	router := mounted(t)
	code, body := send(t, router, http.MethodGet, settings, "", true)
	if code != http.StatusOK {
		t.Fatalf("read defaults: %d %s", code, body)
	}
	var defaults struct {
		PrimaryColor string `json:"primaryColor"`
	}
	if err := json.Unmarshal([]byte(body), &defaults); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]string{
		"title": "Our site", "primaryColor": defaults.PrimaryColor,
	})
	if err != nil {
		t.Fatal(err)
	}
	code, body = send(t, router, http.MethodPut, settings, string(payload), true)
	if code != http.StatusOK {
		t.Fatalf("saving the returned default colour %q: %d %s; want 200", defaults.PrimaryColor, code, body)
	}
}
