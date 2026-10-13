package icon_test

// kernel_binding_test.go binds entity.Icons to this set. The kernel names the
// icons a resource may be about; only ui/icon draws them, and kit may not import
// ui (scripts/check_packages.sh), so what ties the two is a test here and not an
// import there.
//
// It promises three things and only three. Every name the contract admits is a
// name Resolve knows, so adding a 17th without a binding is red. Every alias the
// contract needs names a body, never another alias, because Resolve consults
// aliases exactly once and then bodies. And the set that answers Fallback is
// exactly the list below — a list that can only shrink as glyphs are drawn, and
// that a name therefore cannot join quietly.

import (
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/icon"
)

// undrawn is every contract name this vendored set has no drawing for. The
// design decision about which eight glyphs is design's, and fetching a reviewed
// set is one commit; inventing path data from memory is not an option in a file
// whose whole claim is that every byte below it is Phosphor's and reviewed once.
var undrawn = []string{"folder", "chart", "money", "box", "tag", "building", "location", "link"}

func TestEveryKernelIconNameResolves(t *testing.T) {
	t.Parallel()
	for _, name := range entity.Icons {
		_, known := icon.Resolve(name)
		if !known && !slices.Contains(undrawn, name) {
			// An unknown name answers Fallback rather than nothing, which is the
			// right answer for a toolbar and the wrong one for a contract: a name
			// the vocabulary admits has to be drawn here or named below as owed.
			t.Errorf("Resolve(%q) knows nothing of this name; entity.Icons admits it, and it is not in the list of names this set does not draw", name)
			continue
		}
	}
}

func TestTheUndrawnKernelNamesAnswerTheFallback(t *testing.T) {
	t.Parallel()
	for _, name := range undrawn {
		glyph, _ := icon.Resolve(name)
		if glyph.Name != icon.Fallback {
			t.Errorf("Resolve(%q) answers %q: this name has a drawing now, so remove it from the shrinking list", name, glyph.Name)
		}
	}
}

func TestEveryKernelAliasNamesABody(t *testing.T) {
	t.Parallel()
	// One hop is all Resolve takes, so an alias of an alias answers Fallback —
	// a bug nobody would notice until a resource quietly wore a question mark.
	for _, name := range []string{"task", "person", "people", "settings", "plan", "message", "document", "calendar"} {
		glyph, known := icon.Resolve(name)
		if !known {
			t.Errorf("Resolve(%q) does not resolve; entity.Icons admits it", name)
			continue
		}
		if !slices.Contains(icon.Names(), glyph.Name) {
			t.Errorf("Resolve(%q) answers %q, which is not a body this set draws", name, glyph.Name)
		}
	}
}
