package icon_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/ui/icon"
)

func TestEveryGlyphCarriesItsProvenance(t *testing.T) {
	t.Parallel()
	for _, name := range icon.Names() {
		if glyph, known := icon.Resolve(name); !known || glyph.Source != icon.Source || glyph.License != icon.License {
			t.Errorf("%s provenance: %+v", name, glyph)
		}
	}
	for _, name := range []string{"search", "CHEVRON_DOWN", "unknown"} {
		glyph, _ := icon.Resolve(name)
		canonical, _ := icon.Resolve(glyph.Name)
		if glyph != canonical {
			t.Errorf("%s attribution does not follow the resolved glyph", name)
		}
	}
}

func TestGlyphExportContract(t *testing.T) {
	t.Parallel()
	if icon.ViewBox != "0 0 256 256" {
		t.Fatalf("unexpected coordinate system: %s", icon.ViewBox)
	}
	names := icon.Names()
	if !slices.IsSorted(names) {
		t.Error("export order is not stable")
	}
	names[0] = "consumer edit"
	if icon.Names()[0] == names[0] {
		t.Error("Names returns shared mutable state")
	}
	glyph, _ := icon.Resolve("caret-up")
	encoded, err := json.Marshal(glyph)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]string
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 || fields["name"] != "caret-up" || fields["body"] != glyph.Body || fields["source"] != "github.com/phosphor-icons/core" || fields["license"] != "MIT" {
		t.Fatalf("glyph JSON contract: %s", encoded)
	}
}
