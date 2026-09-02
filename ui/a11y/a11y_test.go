package a11y_test

import (
	"strings"
	"testing"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/a11y"
)

func render(t *testing.T, nodes ...g.Node) string {
	t.Helper()
	var b strings.Builder
	if err := h.Div(nodes...).Render(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestAbsentIsNotFalse(t *testing.T) {
	t.Parallel()
	if got := render(t, a11y.Props{}.Attributes()...); got != "<div></div>" {
		t.Fatalf("the zero Props rendered %q; an unset tri-state must say nothing", got)
	}
	got := render(t, a11y.Props{Expanded: a11y.Bool(false)}.Attributes()...)
	if !strings.Contains(got, `aria-expanded="false"`) {
		t.Fatalf("an explicit false rendered %q", got)
	}
}

func TestAttributesRenderInAFixedOrder(t *testing.T) {
	t.Parallel()
	p := a11y.Props{
		Role: a11y.RoleDialog, Label: "Confirm", DescribedBy: "d",
		Expanded: a11y.Bool(true), Live: "polite", TabIndex: new(int),
	}
	first, second := render(t, p.Attributes()...), render(t, p.Attributes()...)
	if first != second {
		t.Fatal("two renders of one Props differ")
	}
	for _, want := range []string{`role="dialog"`, `aria-label="Confirm"`, `aria-describedby="d"`,
		`aria-expanded="true"`, `aria-live="polite"`, `tabindex="0"`} {
		if !strings.Contains(first, want) {
			t.Errorf("missing %s in %s", want, first)
		}
	}
	if strings.Index(first, "role=") > strings.Index(first, "aria-label=") {
		t.Error("role must come first, so the order is the one documented")
	}
}

func TestIDsAreUnique(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for range 100 {
		id := a11y.ID("field")
		if seen[id] {
			t.Fatalf("a11y.ID repeated %q", id)
		}
		if !strings.HasPrefix(id, "field-") {
			t.Fatalf("a11y.ID lost its prefix: %q", id)
		}
		seen[id] = true
	}
}
