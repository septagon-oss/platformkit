package css_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/css"
)

func TestRuleRendersCustomPropertiesFirstAndSorted(t *testing.T) {
	t.Parallel()
	r := css.Rule{Selector: ":root", Decls: []css.Declaration{
		css.Decl("color", css.Literal("red")),
		css.Decl("--b", css.Literal("2")),
		css.Decl("--a", css.Literal("1")),
	}}
	want := ":root {\n  --a: 1;\n  --b: 2;\n  color: red;\n}"
	if got := r.CSS(); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAddRuleMergesOneSelectorLastWriteWins(t *testing.T) {
	t.Parallel()
	s := css.NewSheet()
	s.Select(".a", css.Decl("color", css.Literal("red")), css.Decl("margin", css.Literal("0")))
	s.Select(".a", css.Decl("color", css.Literal("blue")))
	if n := len(s.Rules()); n != 1 {
		t.Fatalf("two contributions to one selector made %d rules, want 1", n)
	}
	out := s.CSS()
	if strings.Contains(out, "red") || !strings.Contains(out, "blue") || !strings.Contains(out, "margin") {
		t.Fatalf("merge lost or kept the wrong declaration:\n%s", out)
	}
}

func TestVarRefRendersAndRefusesAnEscape(t *testing.T) {
	t.Parallel()
	if got := css.VarRef("pk-color-focus", "").CSS(); got != "var(--pk-color-focus)" {
		t.Fatalf("VarRef rendered %q", got)
	}
	if got := css.VarRef("pk-color-focus", "#fff").CSS(); got != "var(--pk-color-focus, #fff)" {
		t.Fatalf("VarRef with a fallback rendered %q", got)
	}
	for _, bad := range []struct{ name, fallback string }{
		{"Pk-Color", ""},
		{"pk", "red); body { display: none"},
		{"pk", "/* x"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("VarRef(%q, %q) did not panic", bad.name, bad.fallback)
				}
			}()
			css.VarRef(bad.name, bad.fallback)
		}()
	}
}

func TestKeyframeStopsRenderInOffsetOrder(t *testing.T) {
	t.Parallel()
	s := css.NewSheet()
	s.Keyframes("spin", func(k *css.Keyframes) {
		k.At("to", css.Decl("opacity", css.Literal("1")))
		k.At("50%", css.Decl("opacity", css.Literal("0")))
		k.At("from", css.Decl("opacity", css.Literal("1")))
	})
	out := s.CSS()
	from, mid, to := strings.Index(out, "from"), strings.Index(out, "50%"), strings.Index(out, "to {")
	if !(from < mid && mid < to) {
		t.Fatalf("stops are out of order:\n%s", out)
	}
}

func TestMediaNestsAndIndents(t *testing.T) {
	t.Parallel()
	s := css.NewSheet()
	s.Media("(min-width: 40rem)", func(inner *css.Sheet) {
		inner.Select(".a", css.Decl("display", css.Literal("flex")))
	})
	want := "@media (min-width: 40rem) {\n  .a {\n    display: flex;\n  }\n}"
	if got := s.CSS(); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
