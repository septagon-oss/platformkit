package main

// The reference composition is where a declaration becomes a person's control:
// `present.Resolve` answers `savable` from the hint an entry carries, and the
// document this installation serves is what every shell — the browser and the
// native one — draws the heart from. A hint no composition writes and a resolved
// key nothing serves are the same missing half seen from two ends, so this reads
// the document a signed-in person is answered and asks which of its records they
// may keep a copy of.
//
// The two languages are asked for because the declaration's four words are copy:
// an entry that answers English to a Portuguese request has a control no person
// of that tenant was ever offered a word of.

import (
	"net/http"
	"testing"
)

// TestTheReferenceCompositionTellsAPersonWhichOfItsRecordsTheyMayKeep reads the
// served catalogue as a shell reads it: one entry of the composition's own, the
// reference task, and the `savable` value beside it.
func TestTheReferenceCompositionTellsAPersonWhichOfItsRecordsTheyMayKeep(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	startSaved(t, cfg, c)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	acmeID := installationTenantID(t, cfg, admin, "acme")
	declareLocale(t, cfg, admin, acmeID, `{"default":"en","supported":["en","pt-PT"]}`)

	for _, ask := range []struct {
		accept string
		// fallbacks records the four English words this installation answers with
		// when nobody supplies copy; a request in another language is refused them.
		fallbacks map[string]string
	}{
		{accept: "en", fallbacks: map[string]string{}},
		{accept: "pt-PT", fallbacks: map[string]string{
			"save": "Save", "saved": "Saved", "remove": "Remove from saved", "listTitle": "Saved items",
		}},
	} {
		t.Run(ask.accept, func(t *testing.T) {
			status, _, doc := catalogueDocument(t, cfg, admin, ask.accept)
			if status != http.StatusOK {
				t.Fatalf("GET /api/v1/app/resources as a signed-in member with Accept-Language %s = %d", ask.accept, status)
			}
			savable := taskSavable(t, doc)
			if savable == nil {
				t.Fatalf("the served task entry answers no `savable` to a member who may read it, in %s: nothing on this installation tells a person they may keep a copy", ask.accept)
			}
			object, ok := savable.(map[string]any)
			if !ok {
				t.Fatalf("`savable` is %#v, want the declared pair as an object", savable)
			}
			for _, key := range []string{"module", "kind", "keyField", "versionField"} {
				if text, _ := object[key].(string); text == "" {
					t.Errorf("`savable.%s` = %v, want the declared %s the save command posts", key, object[key], key)
				}
			}
			words, _ := object["words"].(map[string]any)
			if len(words) != 4 {
				t.Fatalf("`savable.words` = %#v, want the four words the control and the kept list show", object["words"])
			}
			for _, word := range []string{"save", "saved", "remove", "listTitle"} {
				got, _ := words[word].(string)
				if got == "" {
					t.Errorf("`savable.words.%s` is empty in %s, want a word a person reads", word, ask.accept)
					continue
				}
				if english, listed := ask.fallbacks[word]; listed && got == english {
					t.Errorf("a %s request is answered `savable.words.%s` = %q, the English fallback: the saving words are copy and belong in both catalogues", ask.accept, word, got)
				}
			}
		})
	}
}

// taskSavable is the `savable` value the served document carries for the reference
// task entry, and nil when the entry answers none.
func taskSavable(t *testing.T, doc map[string]any) any {
	t.Helper()
	resources, _ := doc["resources"].([]any)
	for _, raw := range resources {
		entry, ok := raw.(map[string]any)
		if !ok || entry["module"] != "task" || entry["entity"] != "task" {
			continue
		}
		resolved, ok := entry["resolved"].(map[string]any)
		if !ok {
			t.Fatalf("the served task entry answers no `resolved` policy: %#v", entry["resolved"])
		}
		return resolved["savable"]
	}
	t.Fatal("the served document names no task/task entry")
	return nil
}
