package screens_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// memberNames is every property name the published document types as an array or an
// object, at every depth it reaches: the members a Page always writes as [] or {}.
// The names are read off the file the phone's builder is handed, not restated here,
// so a member added to the type joins the check with no edit to this file.
func memberNames(t *testing.T, node map[string]any, prefix string, out *[]string) {
	t.Helper()
	props, ok := node["properties"].(map[string]any)
	if !ok {
		return
	}
	for name, child := range props {
		c, ok := child.(map[string]any)
		if !ok {
			continue
		}
		path := prefix + "." + name
		switch c["type"] {
		case "array", "object":
			*out = append(*out, path)
		}
		if items, ok := c["items"].(map[string]any); ok {
			memberNames(t, items, path+"[]", out)
		}
		memberNames(t, c, path, out)
	}
}

// nullAggregates returns every place the marshalled page writes a null at a member the
// published document types as an array or an object.
func nullAggregates(v any, path string, names []string, hits *[]string) {
	switch typed := v.(type) {
	case map[string]any:
		for k, item := range typed {
			at := path + "." + k
			if slices.Contains(names, at) && item == nil {
				*hits = append(*hits, at)
			}
			nullAggregates(item, at, names, hits)
		}
	case []any:
		for _, item := range typed {
			nullAggregates(item, path+"[]", names, hits)
		}
	}
}

// TestEveryBuiltPageWritesTheAggregatesTheDocumentNames is the promise the published
// document makes to a shell that cannot read Go: each array and object member it names
// is written, wherever the page's kind reaches it, and never as a null.
func TestEveryBuiltPageWritesTheAggregatesTheDocumentNames(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "kit", "entity", "view", "page-v1.schema.json"))
	if err != nil {
		t.Fatalf("the published document is not there to read: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var names []string
	memberNames(t, doc, "", &names)
	slices.Sort(names)
	if len(names) == 0 {
		t.Fatal("the published document names no aggregate member at all")
	}

	for _, c := range readPageCases(t) {
		for _, s := range c.States {
			page, err := c.build(s)
			if err != nil {
				continue // a refused build writes no page to check
			}
			built, err := json.Marshal(page)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(built, &doc); err != nil {
				t.Fatal(err)
			}
			var hits []string
			nullAggregates(doc, "", names, &hits)
			for _, h := range hits {
				t.Errorf("%s %s: the published document types %s as an array or an object, the page writes null",
					filepath.Base(c.File), s.Name, h)
			}
		}
	}
}
