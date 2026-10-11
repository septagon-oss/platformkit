package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
)

// TestEveryUpdateEventCarriesWhatChanged holds the reference composition to "every
// audited change carries what changed": an event that records an update of a row the
// trail keeps (a name ending in "updated") declares a `changes` member, the
// kit/events.Change list, so the trail row says what the save replaced and not only
// what the row became.
func TestEveryUpdateEventCarriesWhatChanged(t *testing.T) {
	_, cfg := configure(t)
	body, err := app.AsyncAPI(compose(cfg).modules)
	if err != nil {
		t.Fatalf("AsyncAPI: %v", err)
	}
	var doc struct {
		Channels map[string]struct {
			Messages map[string]struct {
				Payload struct {
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"payload"`
			} `json:"messages"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	updates := 0
	for name, ch := range doc.Channels {
		if !strings.HasSuffix(name, "updated") {
			continue
		}
		updates++
		if _, ok := ch.Messages[name].Payload.Properties["changes"]; !ok {
			t.Errorf("%s records an update and its payload carries no changes", name)
		}
	}
	if updates == 0 {
		t.Fatal("the composition declares no update event, which proves nothing")
	}
}
