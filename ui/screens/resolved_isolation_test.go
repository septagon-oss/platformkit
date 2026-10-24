package screens_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// The document is built per request, so a language or a permission set cannot
// leak between callers unless the registration is written through. Four
// requests over one registration must give four bodies, each repeatable.
func TestResolvedBodiesDoNotShareStateAcrossLanguagesAndPermissionSets(t *testing.T) {
	t.Parallel()
	r := resource()
	r.Present.Singular = "note"
	r.Commands = []httpx.Command{{Verb: "publish", Summary: "Publish a note"}}
	words := map[string]map[string]string{
		"en":    {},
		"pt-PT": {"hints.note/note.singular": "nota", "present.true": "Sim"},
	}
	body := func(language string, commands bool) string {
		reg := r
		if !commands {
			reg.Commands = nil
		}
		text := func(key, fallback string) string {
			if w, ok := words[language][key]; ok {
				return w
			}
			return fallback
		}
		out, err := json.Marshal(screens.Describe1(screens.Localise(reg, text), true).Resolved)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	seen := map[string]string{}
	for _, language := range []string{"en", "pt-PT"} {
		for _, commands := range []bool{true, false} {
			key := language
			if commands {
				key += "+publish"
			}
			first := body(language, commands)
			if again := body(language, commands); again != first {
				t.Fatalf("%s: a repeat of the same request changed the body", key)
			}
			for other, b := range seen {
				if b == first {
					t.Errorf("%s and %s share one body", key, other)
				}
			}
			seen[key] = first
		}
	}
	if len(r.Commands) != 1 || r.Present.Singular != "note" {
		t.Fatal("a request wrote into the shared registration")
	}
}
