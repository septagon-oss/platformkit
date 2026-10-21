// The two questions the name-reading cases do not ask: whether the sentence the
// refusal prints says what the sheet it
// emits says, and whether the sheet the composition serves ends where the sheet
// the composition reads begins.
//
// The first is the promise the whole change rests on. The client layer is the
// last of the four, and for normal declarations a later layer wins (CSS Cascade
// Layers §6), which is why ARCHITECTURE.md, ADR 0018, ui/README.md, ui/hooks.go:78
// and ui/ui_test.go:114 all say the client layer is the strongest of the four and
// that the refusal — not the ranking — is what protects a kernel component. A
// refusal that tells the refused developer the opposite teaches the next change
// to delete itself.
//
// The second is the other artifact: gallery.css. It carries @layer components
// rules for classes app.css does not carry, the same page links both
// (modules/admin/internal/gallery.go:101), and ui.Assets serves it for every
// composition, including one that never passed components.GalleryClassLists in
// Extra.Lists. Nothing in the tree read its class set against the gate: the served-sheet
// files read app.css's components block, and the sibling case pinned only that gallery's
// rules landing inside a layer.
package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// gallerySheet reads one file out of the asset tree the shell serves, in the
// bytes a browser downloads — the read servedLayeredSheet in served_sheet_layers_test.go makes.
func gallerySheet(t *testing.T, name string) string {
	t.Helper()
	return servedLayeredSheet(t, ui.Assets(ui.Compose(design.Default())), name)
}

// TestEveryClassGalleryCSSAddressesIsRefusedByTheGate is the brief's
// *Done when* read off the second served artifact: every class gallery.css
// addresses in @layer components is a class a kernel component renders, so a
// consumer rule at it lands in the layer that outranks it and must be refused —
// whether or not this composition passed the gallery's own class lists.
func TestEveryClassGalleryCSSAddressesIsRefusedByTheGate(t *testing.T) {
	t.Parallel()
	gallery := gallerySheet(t, "gallery.css")
	heads := servedSubjects(t, servedBlock(t, gallery, "components"), "gallery.css's @layer components")
	checked := 0
	for _, head := range heads {
		for _, written := range classTokensInSelector(t, head) {
			checked++
			if refusal, body := servedRefusal(t, "."+written); refusal == "" {
				t.Errorf("ui.Compose took a consumer rule at .%s: gallery.css addresses it in @layer components (%d-byte sheet), the shell links that sheet beside app.css, and @layer client outranks it; the gate's vocabulary names no class for this selector", written, body)
			}
			if refusal, _ := servedRefusal(t, `[class~="`+written+`"]`); refusal == "" {
				t.Errorf("ui.Compose took a consumer rule at [class~=%q], which matches the same elements gallery.css styles at .%s, from the layer that outranks that rule", written, written)
			}
		}
	}
	if checked == 0 {
		t.Fatal("gallery.css addresses no class in @layer components: this case measures nothing")
	}
	t.Logf("checked %d class tokens gallery.css addresses in @layer components; every one is refused", checked)
}

// TestTheClassRefusalStatesTheDirectionTheSheetDeclares reads the
// sentence the gate shows a refused developer against the order statement the same
// Compose call emits. `@layer a, b;` means b wins normal declarations (§6.4), so a
// message about a rule in components and a rule in client may only say that the
// client rule wins; the sheet's own first line is what says so, and the message
// must agree with it.
func TestTheClassRefusalStatesTheDirectionTheSheetDeclares(t *testing.T) {
	t.Parallel()
	sheet := servedSheet()
	statement := sheet[:strings.Index(sheet, "\n")]
	if !strings.HasPrefix(statement, "@layer ") || !strings.HasSuffix(statement, ";") {
		t.Fatalf("the sheet's first line is not an order statement: %q", statement)
	}
	order := strings.Split(strings.TrimSuffix(strings.TrimPrefix(statement, "@layer "), ";"), ", ")
	components, client := -1, -1
	for i, name := range order {
		switch strings.TrimSpace(name) {
		case "components":
			components = i
		case "client":
			client = i
		}
	}
	if components < 0 || client < 0 {
		t.Fatalf("the order statement names neither component layer: %q", statement)
	}
	if components >= client {
		t.Fatalf("this case's premise moved: @layer components is not declared ahead of @layer client (%v), so the ranking, not the refusal, is what protects a component", order)
	}
	// components is declared earlier, so for a normal declaration the client rule
	// wins and the kernel's rule loses.
	refusal, _ := servedRefusal(t, ".sr-only")
	if refusal == "" {
		t.Fatal("the gate no longer refuses .sr-only, so it states no direction")
	}
	for _, claim := range []string{
		"ranks ahead of the client layer",
		"ranks above the client layer",
		"this rule wins every element the kernel renders that class on",
	} {
		if strings.Contains(refusal, claim) {
			t.Errorf("the refusal says %q, which the sheet's own first line contradicts: %q declares @layer components ahead of @layer client, so for a normal declaration the kernel's rule loses to the consumer's and the refusal exists precisely because the consumer would win. The message must state that direction (%v): a refused developer who reads that the kernel's rule would have won learns that the refusal protects nothing and deletes it", claim, statement, order)
		}
	}
	// The reason must survive its correction: the refusal still has to name both
	// layers whose ranks it is explaining and the class it refused.
	for _, want := range []string{"@layer components", "client layer", `"sr-only"`} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal no longer names %s, so it explains no rank and refuses nothing readable: %s", want, refusal)
		}
	}
}

// TestARawColourIsRefusedInEverySpellingAValueTakes is the palette
// half of the same promise. ARCHITECTURE.md and ADR 0018 both say Compose refuses
// a consumer rule that carries "a raw colour", and rawColourRE's comment says it
// matches "the ways a rule says a colour without naming a token". Since rgb() the
// CSS language gained colour functions, and ADR 0018 evaluated a candidate palette
// written in one of them. A rule may state a colour in those spellings without
// naming a token, so it must be refused in them too — and a spelling that names a
// token must stay legal, which is what the controls below hold a fix to.
func TestARawColourIsRefusedInEverySpellingAValueTakes(t *testing.T) {
	t.Parallel()
	for _, colour := range []string{
		"#3366cc", "rgb(51 102 204)", "hsl(220 60% 50%)", // refused today
		"oklch(55% 0.15 250)", "oklab(0.55 0.1 -0.1)", "lab(55% 20 -30)",
		"lch(55% 30 250)", "hwb(250 20% 20%)", "color(display-p3 0.2 0.4 0.8)",
		"color-mix(in oklab, var(--pk-color-accent-default), white 20%)",
	} {
		sheet := css.NewSheet().Select(".store-hero", css.Decl("color", css.Literal(colour)))
		var refusal string
		func() {
			defer func() {
				if r := recover(); r != nil {
					refusal = fmt.Sprint(r)
				}
			}()
			ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}})
		}()
		if refusal == "" {
			t.Errorf("ui.Compose took the raw colour %q in a consumer rule: a browser computes a colour from it, so it names no token and the palette is spelled in the client layer, which is what the gate's raw-colour read exists to refuse", colour)
			continue
		}
		if !strings.Contains(refusal, "raw colour") {
			t.Errorf("the refusal for %q does not say why: %s", colour, refusal)
		}
	}
	// What a consumer owns must keep composing, or the fix is a ban on colour.
	for _, value := range []string{
		"var(--pk-color-accent-default)",
		"var(--pk-color-surface, transparent)",
		"currentColor", "inherit", "transparent", "revert-layer",
	} {
		sheet := css.NewSheet().Select(".store-hero", css.Decl("color", css.Literal(value)))
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ui.Compose refused a consumer value that names a token or a keyword, %q: %v", value, r)
				}
			}()
			ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}})
		}()
	}
}
