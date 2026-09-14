package examples_test

// closure_test.go closes the loop the gallery promises: every class a renderer
// can emit is declared in ui/components/classlists.go and resolves to a CSS
// rule, the rendered HTML carries the accessibility structure the contracts
// imply, and no component emits an inline handler the content security policy
// would drop. These cases render Gallery, so they live beside it; the renderer
// tests that read unexported class lists stay in ui/components.

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/style"
)

func renderNodeToString(t *testing.T, node g.Node) string {
	t.Helper()
	var output strings.Builder
	if err := node.Render(&output); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

// gallery is the package's own registry, rendered. See gallery.go: the tests
// and /admin/_gallery read one list, so a component the shell can show is a
// component whose classes the closure test below has seen.
func gallery() []g.Node {
	out := make([]g.Node, 0, len(examples.Gallery()))
	for _, example := range examples.Gallery() {
		out = append(out, example.Node)
	}
	return out
}

func renderAll(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, n := range gallery() {
		if n == nil {
			continue
		}
		if err := n.Render(&b); err != nil {
			t.Fatalf("render: %v", err)
		}
		b.WriteString("\n")
	}
	return b.String()
}

var classAttrRE = regexp.MustCompile(`class="([^"]*)"`)

// TestRenderedClassesAreDeclared is the architectural loop-closure: every
// class in rendered HTML must be covered by the stylesheet derived from
// components.ClassLists(). A renderer inventing a class the lists do not declare, or a
// list drifting from a renderer, fails here — which is exactly the failure
// Tailwind users hit at runtime as silently unstyled markup.
func TestRenderedClassesAreDeclared(t *testing.T) {
	t.Parallel()

	sheet, err := style.For(components.ClassLists()...)
	if err != nil {
		t.Fatalf("style.For over declared lists: %v", err)
	}
	css := sheet.CSS()

	html := renderAll(t)
	seen := map[string]bool{}
	for _, m := range classAttrRE.FindAllStringSubmatch(html, -1) {
		for _, class := range strings.Fields(m[1]) {
			seen[class] = true
		}
	}
	if len(seen) < 60 {
		t.Fatalf("only %d distinct classes rendered; the gallery regressed", len(seen))
	}

	var missing []string
	for class := range seen {
		// HTMX owns this runtime sentinel and its visibility rule; it is not a
		// visual utility and therefore deliberately does not enter tw style.
		if class == "htmx-indicator" {
			continue
		}
		if _, err := style.Rules(class); err != nil {
			missing = append(missing, class+" (unresolvable)")
			continue
		}
		// The class must be in the derived sheet, not merely resolvable —
		// prefix-escaped for selector matching.
		esc := strings.NewReplacer(":", "\\:", "/", "\\/", "[", "\\[", "]", "\\]", ".", "\\.").Replace(class)
		selector := regexp.MustCompile(regexp.QuoteMeta("."+esc) + `(?:[\s:{,.>+~]|$)`)
		if !selector.MatchString(css) {
			missing = append(missing, class+" (not in For(ClassLists()) sheet)")
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("rendered class not backed by the design system: %s", m)
	}
}

func TestAccessibilityStructure(t *testing.T) {
	t.Parallel()
	captures := map[string]examples.Example{}
	for _, example := range examples.Gallery() {
		captures[example.ID] = example
	}
	for id, wants := range map[string][]string{
		"pk-ui.component.input/invalid":      {`aria-invalid="true"`, `aria-describedby="pk-input-slug-error"`},
		"pk-ui.component.input/email":        {`for="pk-input-email"`},
		"pk-ui.component.alert/danger":       {`role="alert"`},
		"pk-ui.component.alert/info":         {`role="status"`},
		"pk-ui.component.breadcrumb/default": {`aria-current="page"`, `aria-label="Breadcrumb"`},
		"pk-ui.component.pagination/default": {`aria-current="page"`, `aria-label="Pagination, twelve pages"`},
		"pk-ui.component.sidebar/collapsed":  {`aria-label="Sidebar example, collapsed"`},
		"pk-ui.component.tabs/default":       {`aria-selected="true"`},
		"pk-ui.component.button/with-icon":   {`aria-hidden="true"`},
		"pk-ui.component.link/external":      {`rel="noopener noreferrer"`},
		"pk-ui.component.button/loading":     {`aria-busy="true"`},
		"pk-ui.component.table/default":      {`scope="col"`},
	} {
		t.Run(id, func(t *testing.T) {
			example, ok := captures[id]
			if !ok {
				t.Fatalf("the gallery is missing %s", id)
			}
			html := renderNodeToString(t, example.Node)
			for _, want := range wants {
				if !strings.Contains(html, want) {
					t.Errorf("rendered example missing accessibility structure %s", want)
				}
			}
		})
	}
}

func TestNoComponentEmitsAnInlineHandler(t *testing.T) {
	t.Parallel()
	for _, example := range examples.Gallery() {
		var b strings.Builder
		if err := example.Node.Render(&b); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(b.String(), " onclick=") || strings.Contains(b.String(), "javascript:") {
			t.Fatalf("%s emits an inline handler, which script-src 'self' 'nonce-…' blocks:\n%s", example.Name, b.String())
		}
	}
	var b strings.Builder
	_ = components.ConfirmDialog(components.ConfirmDialogProps{}).Render(&b)
	if strings.Contains(b.String(), "onclick") {
		t.Fatal("the confirm dialog's cancel button has an inline handler; confirm.js closes it")
	}
}
