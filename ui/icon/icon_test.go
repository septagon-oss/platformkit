package icon_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/icon"
)

func TestResolveKnownAliasAndFallback(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"trash", "Trash", " trash ", "TRASH"} {
		if g, ok := icon.Resolve(name); !ok || g.Name != "trash" {
			t.Errorf("Resolve(%q) = %q, %v", name, g.Name, ok)
		}
	}
	if g, ok := icon.Resolve("search"); !ok || g.Name != "magnifying-glass" {
		t.Errorf("the search alias resolved to %q, %v", g.Name, ok)
	}
	g, ok := icon.Resolve("no-such-glyph")
	if ok {
		t.Error("an unknown name reported itself as known")
	}
	if g.Name != icon.Fallback || g.Body == "" {
		t.Errorf("an unknown name fell back to %q with a %d-byte body", g.Name, len(g.Body))
	}
}

func TestEveryGlyphIsDrawableMarkup(t *testing.T) {
	t.Parallel()
	names := icon.Names()
	if len(names) < 20 {
		t.Fatalf("the set has %d glyphs; something stopped being registered", len(names))
	}
	for _, name := range names {
		g, ok := icon.Resolve(name)
		if !ok {
			t.Errorf("%s is listed by Names and does not resolve", name)
			continue
		}
		if !strings.HasPrefix(g.Body, "<") || !strings.HasSuffix(strings.TrimSpace(g.Body), ">") {
			t.Errorf("%s does not look like markup: %q", name, g.Body)
		}
		if strings.Count(g.Body, "<") != strings.Count(g.Body, ">") {
			t.Errorf("%s has unbalanced angle brackets", name)
		}
	}
}

func TestCaretsAreOneShapeTurned(t *testing.T) {
	t.Parallel()
	down, _ := icon.Resolve("caret-down")
	for _, name := range []string{"caret-up", "caret-left", "caret-right"} {
		g, ok := icon.Resolve(name)
		if !ok {
			t.Fatalf("%s is missing", name)
		}
		if !strings.Contains(g.Body, down.Body) {
			t.Errorf("%s is not the caret path rotated", name)
		}
		if !strings.Contains(g.Body, "rotate(") {
			t.Errorf("%s carries no rotation", name)
		}
	}
}
