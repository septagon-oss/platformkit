package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
)

func TestTranslationEventDocumentsItsPublishingApp(t *testing.T) {
	stored, err := os.ReadFile(asyncapiGolden)
	if err != nil {
		t.Fatal(err)
	}
	_, cfg := configure(t)
	rendered, err := app.AsyncAPI(compose(cfg).modules)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{"stored": stored, "rendered": rendered} {
		t.Run(name, func(t *testing.T) {
			var doc struct {
				Channels map[string]struct {
					Description string `json:"description"`
				} `json:"channels"`
			}
			if err := json.Unmarshal(body, &doc); err != nil {
				t.Fatal(err)
			}
			channel, ok := doc.Channels["translation.updated"]
			if !ok {
				t.Fatal("the composed translation event has no channel")
			}
			for _, phrase := range []string{"CloudEvents", "`app` names the app whose process published it"} {
				if !strings.Contains(channel.Description, phrase) {
					t.Errorf("the translation channel omits %q: %s", phrase, channel.Description)
				}
			}
		})
	}
}
