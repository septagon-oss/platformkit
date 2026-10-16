package forms

import (
	"encoding/json"
	"io/fs"
	"testing"
)

func TestRichTextCataloguesHaveMatchingLanguages(t *testing.T) {
	keys := map[string]map[string]json.RawMessage{}
	for _, language := range []string{"en", "pt-PT"} {
		data, err := fs.ReadFile(Catalogues(), language+".json")
		if err != nil {
			t.Fatalf("%s rich-text catalogue: %v", language, err)
		}
		var messages map[string]json.RawMessage
		if err := json.Unmarshal(data, &messages); err != nil {
			t.Fatal(err)
		}
		keys[language] = messages
	}
	for key := range keys["pt-PT"] {
		if _, ok := keys["en"][key]; !ok {
			t.Errorf("English catalogue lacks %q", key)
		}
	}
	for key := range keys["en"] {
		if _, ok := keys["pt-PT"][key]; !ok {
			t.Errorf("Portuguese catalogue lacks %q", key)
		}
	}
}
