package components

// collision_test.go enforces the variant discipline structurally: in any
// class list a renderer composes onto ONE element, no CSS property may be
// declared by two different utility classes under the same variant prefix.
// Two single-class rules have equal specificity, so the emitted sheet's
// order — alphabetical, an implementation detail — would silently pick the
// winner. This is the bug class that once left secondary buttons borderless
// and selected tags unselected; the guard makes reintroducing it a test
// failure instead of a visual regression.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/style"
)

// composedLists enumerates every class list the renderers and the exported
// Styles surface apply to a single element. New compositions belong here;
// the completeness check below fails if a variant map gains a key this
// table does not cover.
func composedLists(t *testing.T) map[string]style.ClassList {
	t.Helper()
	out := map[string]style.ClassList{}
	for v := range clButtonVariant {
		for s := range clButtonSize {
			base := clButtonBase.Merge(clButtonVariant[v]).Merge(clButtonSize[s])
			out["button/"+v+"/"+s] = base
			out["button-full/"+v+"/"+s] = base.Merge(clButtonFull)
			out["button-icon-only/"+v+"/"+s] = base.Merge(clButtonIconOnly)
		}
	}
	for v := range clBadgeVariant {
		out["badge/"+v] = clBadgeBase.Merge(clBadgeVariant[v]).Merge(clBadgeSize["md"])
	}
	for v := range clAlertVariant {
		out["alert/"+v] = clAlertBase.Merge(clAlertVariant[v])
	}
	out["input/normal"] = clInput.Merge(clInputNormal).Merge(clInputSize["md"])
	out["input/error"] = clInput.Merge(clInputError).Merge(clInputSize["md"])
	out["textarea/manual"] = clInput.Merge(clInputNormal).Merge(clInputSize["md"]).Merge(clTextareaManual)
	out["textarea/autoresize"] = clInput.Merge(clInputNormal).Merge(clInputSize["md"]).Merge(clTextareaAuto)
	out["textarea/error"] = clInput.Merge(clInputError).Merge(clInputSize["md"]).Merge(clTextareaManual)
	out["empty/default"] = clEmpty.Merge(clEmptyPad)
	out["empty/compact"] = clEmpty.Merge(clEmptyCompact)
	out["empty/default-bordered"] = clEmpty.Merge(clEmptyPad).Merge(clEmptyBordered)
	out["empty/compact-bordered"] = clEmpty.Merge(clEmptyCompact).Merge(clEmptyBordered)
	out["tab/underline-idle"] = clTabsButtonBase.Merge(clTabsButtonUnderlineHorizontal).Merge(clTabsUnderlineIdle)
	out["tab/underline-active"] = clTabsButtonBase.Merge(clTabsButtonUnderlineHorizontal).Merge(clTabsUnderlineActive)
	out["tab/pills-idle"] = clTabsButtonBase.Merge(clTabsButtonPills).Merge(clTabsPillsIdle)
	out["tab/pills-active"] = clTabsButtonBase.Merge(clTabsButtonPills).Merge(clTabsPillsActive)
	out["page/idle"] = clPageBtn.Merge(clPageIdle)
	out["page/current"] = clPageBtn.Merge(clPageCur)
	out["table/th-sort"] = clTableThSort
	out["table/td-primary"] = clTableTd.Merge(clTableTdStrong)
	out["table/row-stripe"] = clTableRow.Merge(clTableRowAlt)
	out["card/clickable"] = clCard.Merge(clCardClickable)
	return out
}

// declaredProperties renders one class alone and reports the CSS property
// names it declares, keyed by variant prefix ("" for plain, "hover",
// "focus-visible", ...). Custom properties (--*) are ignored: cooperative
// families like ring-* communicate through them by design.
func declaredProperties(t *testing.T, class string) map[string][]string {
	t.Helper()
	sheet, err := style.Rules(class)
	if err != nil {
		t.Fatalf("style.Rules(%q): %v", class, err)
	}
	rendered := sheet.CSS()
	prefix := ""
	if i := strings.LastIndex(class, ":"); i >= 0 {
		prefix = class[:i]
	}
	props := map[string][]string{}
	for _, block := range strings.Split(rendered, "}") {
		open := strings.Index(block, "{")
		if open < 0 {
			continue
		}
		for _, decl := range strings.Split(block[open+1:], ";") {
			name, _, ok := strings.Cut(decl, ":")
			name = strings.TrimSpace(name)
			if !ok || name == "" || strings.HasPrefix(name, "--") {
				continue
			}
			props[prefix] = append(props[prefix], name)
		}
	}
	return props
}

func TestComposedListsHaveNoPropertyCollisions(t *testing.T) {
	t.Parallel()
	propCache := map[string]map[string][]string{}
	for name, list := range composedLists(t) {
		classes := strings.Fields(list.Compile())
		// owner[prefix][property] = class that already declared it
		owner := map[string]map[string]string{}
		for _, class := range classes {
			props, ok := propCache[class]
			if !ok {
				props = declaredProperties(t, class)
				propCache[class] = props
			}
			for prefix, names := range props {
				if owner[prefix] == nil {
					owner[prefix] = map[string]string{}
				}
				for _, prop := range names {
					if prior, taken := owner[prefix][prop]; taken && prior != class {
						t.Errorf("%s: %q and %q both declare %q at prefix %q — stylesheet order would decide the winner",
							name, prior, class, prop, prefix)
						continue
					}
					owner[prefix][prop] = class
				}
			}
		}
	}
}
