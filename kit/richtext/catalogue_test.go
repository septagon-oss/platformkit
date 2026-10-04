package richtext_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"testing"

	"github.com/septagon-oss/platformkit/kit/richtext"
)

// TestCataloguesHoldOneKeySetForEveryLanguage is decision 0012's requirement that
// a refusal says the same thing in both catalogues: the second language may not
// be a subset, because a key it lacks silently becomes English at the reader.
func TestCataloguesHoldOneKeySetForEveryLanguage(t *testing.T) {
	keys := map[string]map[string]json.RawMessage{}
	for _, language := range []string{"en", "pt-PT"} {
		data, err := fs.ReadFile(richtext.Catalogues(), language+".json")
		if err != nil {
			t.Fatalf("%s rich-text catalogue: %v", language, err)
		}
		var messages map[string]json.RawMessage
		if err := json.Unmarshal(data, &messages); err != nil {
			t.Fatal(err)
		}
		keys[language] = messages
	}
	for key := range keys["en"] {
		if _, ok := keys["pt-PT"][key]; !ok {
			t.Errorf("Portuguese catalogue lacks %q", key)
		}
	}
	for key := range keys["pt-PT"] {
		if _, ok := keys["en"][key]; !ok {
			t.Errorf("English catalogue lacks %q", key)
		}
	}
}

// TestEveryRefusalNamesItsOwnKey keeps the vocabulary and the catalogue in one
// place: a refusal a reader can be shown must be a refusal an application can
// translate, and one nobody asked for is a key no catalogue will ever hold.
func TestEveryRefusalNamesItsOwnKey(t *testing.T) {
	for _, source := range []string{
		"# Title", "\n<script>alert(1)</script>", "[x](javascript:alert(1))", "![x](https://x.test/a.png)",
		"a ![x](https://x.test/a.png) b", "| a |\n| - |\n| ![x](https://x.test/a.png) |",
		"- a\n  - b\n    - c\n      - d", "[^n]", ":smile:", "$x$", ": a",
	} {
		_, err := richtext.Normalise(source)
		refused, ok := errors.AsType[*richtext.Refused](err)
		if !ok {
			t.Fatalf("%q was not refused: %v", source, err)
		}
		for _, issue := range refused.Issues {
			if issue.Key() == "" {
				t.Errorf("the refusal for %q (%s) has no message key", source, issue.Construct)
			}
		}
	}
}
