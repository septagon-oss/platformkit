package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestContentDeclaresTheLocalizedDraftReviewDeck(t *testing.T) {
	resources := referenceResources(t)
	if bad := httpx.CheckRowHints(resources); bad != "" {
		t.Fatal(bad)
	}
	var content httpx.Resource
	for _, r := range resources {
		if r.Module == "content" && r.Entity == "content" {
			content = r
		}
	}
	for _, language := range []string{"en", "pt-PT"} {
		e := screens.Describe1(screens.Localise(content, words(t, language)), true)
		if e.Presentation == nil || e.Presentation.Deck == nil {
			t.Fatal("content has no deck")
		}
		got, err := json.MarshalIndent(e, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, '\n')
		golden := "testdata/catalog-content-" + language + ".json"
		if os.Getenv("UPDATE_GOLDEN") == "1" {
			if err := os.WriteFile(golden, got, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("%s differs:\n%s", golden, got)
		}
		d := e.Presentation.Deck
		if d.Order != "updatedAt" || len(d.Queue) != 1 || d.Queue[0] != "status:draft" || len(d.Directions) != 3 || !d.Directions["down"].Skip {
			t.Fatalf("deck=%+v", d)
		}
		if language == "pt-PT" && (d.Directions["right"].Label != "Publicar" || d.Directions["left"].Label != "Arquivar") {
			t.Fatalf("labels=%+v", d.Directions)
		}
		for _, c := range e.Commands {
			if c.Verb == "archive" && (c.Presentation == nil || c.Presentation.Confirmation == nil) {
				t.Fatal("archive confirmation lost")
			}
		}
		declared, err := json.Marshal(e.Presentation)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(declared), "undoable") {
			t.Fatal(string(declared))
		}
	}
	content.Commands = nil
	d := screens.Describe1(content, false).Presentation.Deck
	if len(d.Directions) != 1 || !d.Directions["down"].Skip {
		t.Fatalf("read-only directions=%+v", d.Directions)
	}
}
