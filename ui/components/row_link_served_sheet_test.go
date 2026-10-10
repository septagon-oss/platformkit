package components

// A generated row's one way in is its identity link, and that link's hit target
// comes from the served sheet, never from how its label happens to wrap: without
// the floor the anchor is a non-replaced inline box exactly as tall as one line
// of text — 20px beside a title that fits one line — under the 24px floor the
// browser audit enforces. So the contract this test pins: RowLink's own classes
// declare display: inline-block and min-height: 1.5rem, and the sheet the shell
// serves styles every one of them. It fails when the identity anchor is handed a
// class list without the floor, or when a class it renders stops being served.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/style"
)

func TestARowLinksTargetIsSizedByTheServedSheet(t *testing.T) {
	t.Parallel()
	sheet, err := style.For(ShellClassLists()...)
	if err != nil {
		t.Fatal(err)
	}
	html := renderNodeToString(t, RowLink(LinkProps{Label: "Inspect chiller 2", Href: "/app/task/tasks/1"}))
	_, attrs, found := strings.Cut(html, `class="`)
	classes, _, _ := strings.Cut(attrs, `"`)
	if !found || classes == "" {
		t.Fatalf("the row's link renders no class of its own: %s", html)
	}
	css := sheet.CSS()
	var minHeight, display bool
	for class := range strings.FieldsSeq(classes) {
		// A state-variant class escapes its colon and carries a pseudo suffix,
		// so the selector is matched up to the class name, not up to its brace.
		selector := "." + strings.ReplaceAll(class, ":", `\:`)
		_, rule, styled := strings.Cut(css, selector)
		if !styled || rule == "" || (rule[0] != ' ' && rule[0] != ':') {
			t.Errorf("the shell's served sheet styles no %q: the row's target would be its label's height", class)
			continue
		}
		_, rule, _ = strings.Cut(rule, "{")
		rule, _, _ = strings.Cut(rule, "}")
		if strings.Contains(rule, "min-height: 1.5rem") {
			minHeight = true
		}
		if strings.Contains(rule, "display: inline-block") {
			display = true
		}
	}
	if !minHeight {
		t.Error("no served class of the row's link declares min-height: 1.5rem, the audit's 24px floor at root 16px")
	}
	if !display {
		t.Error("no served class of the row's link declares display: inline-block, without which an anchor ignores its min-height")
	}
}
