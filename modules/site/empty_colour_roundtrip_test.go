package site_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

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
