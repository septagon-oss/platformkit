package screens_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestFormAdapterPreservesExplicitWireValuesAndLegacyIdentity(t *testing.T) {
	t.Parallel()
	row := map[string]any{"title": "", "rank": float64(0), "pinned": false, "tags": []any{"alpha", "beta"}}
	errors := map[string]string{"title": "A title is required"}
	form := screens.FormExample("source/distinct-from-dom", resource(), opts, "/save", "Edit note", row, errors, "Refused", false)
	before, err := form.Describe()
	if err != nil {
		t.Fatal(err)
	}
	if before.ID != "source/distinct-from-dom" || !strings.Contains(before.HTML, `id="screen-form"`) ||
		!strings.Contains(before.HTML, `hx-target="#screen-form"`) || !strings.Contains(before.HTML, `for="pk-input-title"`) {
		t.Fatal("source identity changed the legacy form's DOM contract")
	}
	for name, expected := range map[string]string{"title": "", "rank": "0", "tags": "alpha, beta"} {
		field, err := form.At([]string{form.ID, "field/" + name})
		if err != nil {
			t.Fatal(err)
		}
		description, err := field.Describe()
		if err != nil {
			t.Fatal(err)
		}
		var props struct{ Value string }
		if err := json.Unmarshal(description.Props, &props); err != nil || props.Value != expected {
			t.Fatalf("%s wire value = %q, want %q: %v", name, props.Value, expected, err)
		}
	}
	if strings.Contains(before.HTML, " checked") {
		t.Fatal("the false wire value became a checked checkbox")
	}
	row["title"], errors["title"] = "Changed", "Changed"
	after, err := form.Describe()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("the screen adapter retained mutable input maps: %v", err)
	}
}
