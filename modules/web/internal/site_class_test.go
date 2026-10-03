package internal

// The `.name` spelling, read through the vocabulary the client gate uses.
//
// `ui.kernelClasses` is computed from `components.ClassLists()`, and the
// completeness pin the delivery shipped (ui/hooks_test.go) walks the sheets
// `rules(components.ClassLists())`, `componentState()` and `base()` produce. This
// package adds rules to @layer components: `Compose` resolves `Extra.Lists` into
// the same layer, and mount.go passes `lists()` — the page, the header, the
// brand, the logo, the title, the navigation, the main column and the footer —
// beside its prose sheet. The classes those lists compile to are therefore in the
// kernel's own layer of the sheet every page of this application serves, and a
// name the vocabulary omits is a client rule that ranks above the rule this
// module wrote for it.
//
// The check runs on the sheet this module actually composes, because that is the
// artifact a person loads: for every class it carries in @layer components, the
// gate must refuse a consumer rule written at that class. Nothing here reads the
// refusal's absence to decide anything — the premise comes from the served sheet.

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// layerHoldingText returns the layer whose block holds text, by the emitter's
// own convention: a layer block opens and closes at column 0.
func layerHoldingText(sheet, text string) string {
	layer := ""
	for _, line := range strings.Split(sheet[:strings.Index(sheet, text)], "\n") {
		if strings.HasPrefix(line, "@layer ") && strings.HasSuffix(line, " {") {
			layer = strings.TrimSuffix(strings.TrimPrefix(line, "@layer "), " {")
		} else if line == "}" {
			layer = ""
		}
	}
	return layer
}

func TestEveryClassTheSiteServesIsRefusedToAConsumerSheet(t *testing.T) {
	// The premise: this module's own Extra.Lists are the site's markup, and the
	// sheet it serves carries their classes in @layer components.
	served := string(ui.Compose(design.Default(), ui.Extra{Lists: lists(), Sheets: []*css.Sheet{prose()}}).Body)
	classes := map[string]bool{}
	for _, cl := range lists() {
		for _, name := range strings.Fields(cl.Compile()) {
			classes[name] = true
		}
	}
	if len(classes) == 0 {
		t.Fatal("the site renders no classes: this case would check nothing")
	}
	names := make([]string, 0, len(classes))
	for name := range classes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		selector := "." + name
		if strings.ContainsAny(name, `:/.`) {
			t.Skipf("%s needs the escape a stylesheet writes; this site declares none", selector)
		}
		if !strings.Contains(served, selector+" {") {
			t.Errorf("premise: %s is carried by this module's markup and named by no rule of the sheet it serves", selector)
			continue
		}
		if layer := layerHoldingText(served, selector+" {"); layer != "components" {
			t.Errorf("premise: %s is emitted in layer %q, not the components layer", selector, layer)
			continue
		}
		rule := css.NewSheet()
		rule.Select(selector, css.Decl("color", css.VarRef("pk-color-accent-default", "")))
		var refusal string
		func() {
			defer func() {
				if r := recover(); r != nil {
					refusal = fmt.Sprint(r)
				}
			}()
			ui.Compose(design.Default(), ui.Extra{Lists: lists(), Sheets: []*css.Sheet{rule}})
		}()
		if refusal == "" {
			t.Errorf("ui.Compose took a consumer rule at %s, which this module's own markup carries and Compose emits in @layer components of the sheet every page serves: the client layer ranks last, so the consumer's rule wins the site's element, and no class vocabulary lists %q", selector, name)
		}
	}
}
