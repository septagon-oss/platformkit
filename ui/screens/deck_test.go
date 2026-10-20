package screens_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func reviewResource() httpx.Resource {
	r := resource()
	r.Present = entity.EntryHints{PrimaryField: "title", Sortable: []string{"title"}, Deck: &entity.DeckHints{
		Queue: []string{"status:open"}, Order: "-title", Directions: map[string]entity.DeckDirection{
			"right": {Command: "publish", Label: "Publish", Tone: "success", Icon: "document"},
			"left":  {Command: "archive", Label: "Archive", Tone: "warning", Icon: "folder"},
			"up":    {Command: "publish", Label: "Keep", Tone: "info", Icon: "tag"},
			"down":  {Command: "later", Label: "Later", Tone: "neutral", Icon: "task"},
		},
	}}
	for _, verb := range []string{"publish", "archive", "later"} {
		r.Commands = append(r.Commands, httpx.Command{Verb: verb})
	}
	return r
}

func TestDeckCatalogueGolden(t *testing.T) {
	r := reviewResource()
	doc := screens.Catalog{Version: screens.CatalogVersion, Resources: []screens.Entry{screens.Describe1(r, true)}}
	got, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const golden = "testdata/catalog-deck.json"
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
		t.Fatalf("deck catalogue differs from %s:\n%s", golden, got)
	}
	if strings.Contains(string(got), "undoable") || strings.Contains(string(got), "resolved") {
		t.Fatal(string(got))
	}
}

func TestDeckProjectionIsSparseAndKeepsAnEmptyDirectionObject(t *testing.T) {
	r := reviewResource()
	r.Present.Deck.Queue = nil
	r.Commands = nil
	got, err := json.Marshal(screens.Describe1(r, false).Presentation)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"deck":{"order":"-title","directions":{}},"primaryField":"title","sortable":["title"]}` {
		t.Fatal(string(got))
	}
	r.Present.Deck.Directions = map[string]entity.DeckDirection{"down": {Skip: true}}
	got, err = json.Marshal(screens.Describe1(r, false).Presentation)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"directions":{"down":{"skip":true}}`) {
		t.Fatal(string(got))
	}
}

func TestDeckLocalizationAndPermissionProjectionNeverMutateRegistration(t *testing.T) {
	r := reviewResource()
	original := r.Present.Deck.Clone()
	var wg sync.WaitGroup
	for range 30 {
		for _, verb := range []string{"publish", "archive"} {
			wg.Go(func() {
				local := r
				local.Commands = []httpx.Command{{Verb: verb}}
				local = screens.Localise(local, func(key, fallback string) string {
					if strings.HasPrefix(key, "hints.note/note.deck.") {
						return "translated " + fallback
					}
					return fallback
				})
				out := screens.Describe1(local, true).Presentation.Deck
				for key, d := range out.Directions {
					if d.Command != verb {
						t.Errorf("%s leaked %s", verb, d.Command)
					}
					if !strings.HasPrefix(d.Label, "translated ") {
						t.Errorf("untranslated %s", d.Label)
					}
					delete(out.Directions, key)
				}
				out.Queue[0] = "changed"
			})
		}
	}
	wg.Wait()
	if !reflect.DeepEqual(r.Present.Deck, original) {
		t.Fatalf("registration mutated: %+v", r.Present.Deck)
	}
}
