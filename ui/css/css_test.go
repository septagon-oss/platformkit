package css_test

import (
	"errors"
	"slices"
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

func TestAdjacentSelectorContributionsKeepDeclarationOrder(t *testing.T) {
	t.Parallel()
	s := css.NewSheet()
	s.Select(".a", css.Decl("color", css.Literal("red")), css.Decl("margin", css.Literal("0")))
	s.Select(".a", css.Decl("color", css.Literal("blue")))
	if n := len(s.Rules()); n != 1 {
		t.Fatalf("two contributions to one selector made %d rules, want 1", n)
	}
	want := ".a {\n  color: red;\n  margin: 0;\n  color: blue;\n}"
	if got := s.CSS(); got != want {
		t.Fatalf("adjacent contributions must retain CSS declaration order:\n%s", got)
	}
}

func TestSelectorContributionsDoNotCrossInterveningRules(t *testing.T) {
	t.Parallel()
	first := css.NewSheet().Select(".a", css.Decl("color", css.Literal("red")), css.Decl("background", css.Literal("white")))
	middle := css.NewSheet().Select(".b", css.Decl("color", css.Literal("green")), css.Decl("background", css.Literal("black")))
	last := css.NewSheet().Select(".a", css.Decl("color", css.Literal("blue")))
	originals := []string{first.CSS(), middle.CSS(), last.CSS()}
	want := ".a {\n  color: red;\n  background: white;\n}\n\n.b {\n  color: green;\n  background: black;\n}\n\n.a {\n  color: blue;\n}"
	for _, sheet := range []*css.Sheet{
		css.NewSheet().Merge(first).Merge(middle).Merge(last),
		css.NewSheet().Merge(first).Merge(css.NewSheet().Merge(middle).Merge(last)),
		css.NewSheet().Select(".a", first.Rules()[0].Decls...).Select(".b", middle.Rules()[0].Decls...).Select(".a", last.Rules()[0].Decls...),
	} {
		if got := sheet.CSS(); got != want {
			t.Fatalf("a later colour override must not move before .b or promote the earlier background:\n%s", got)
		}
	}
	for i, sheet := range []*css.Sheet{first, middle, last} {
		if sheet.CSS() != originals[i] {
			t.Fatalf("composition changed input sheet %d", i)
		}
	}
}

func TestAdjacentDeclarationsRetainPriorityFallbacksAndShorthands(t *testing.T) {
	t.Parallel()
	for _, declarations := range [][]css.Declaration{
		{css.Decl("color", css.Literal("red !important")), css.Decl("color", css.Literal("blue"))},
		{css.Decl("color", css.Literal("red")), css.Decl("color", css.Literal("not-a-color"))},
		{css.Decl("margin-left", css.Literal("7px")), css.Decl("margin", css.Literal("0")), css.Decl("margin-left", css.Literal("5px"))},
	} {
		sheet := css.NewSheet()
		for _, declaration := range declarations {
			sheet.Select(".a", declaration)
		}
		want := (css.Rule{Selector: ".a", Decls: declarations}).CSS()
		if got := sheet.CSS(); got != want {
			t.Fatalf("only the browser can decide declaration validity and priority; got:\n%s\nwant:\n%s", got, want)
		}
	}
}

func TestSheetOwnsItsDeclarationsAndReturnsDetachedRules(t *testing.T) {
	t.Parallel()
	input := []css.Declaration{css.Decl("color", css.Literal("red"))}
	sheet := css.NewSheet().Select(".a", input...)
	input[0] = css.Decl("color", css.Literal("blue"))
	want := ".a {\n  color: red;\n}"
	if sheet.CSS() != want {
		t.Fatal("editing a caller declaration changed the sheet")
	}
	rules := sheet.Rules()
	rules[0].Selector = ".other"
	rules[0].Decls[0] = css.Decl("color", css.Literal("green"))
	if sheet.CSS() != want {
		t.Fatal("editing a returned rule changed the sheet")
	}
	copy := css.NewSheet().Merge(sheet)
	sheet.Select(".a", css.Decl("color", css.Literal("black")))
	if copy.CSS() != want {
		t.Fatal("later input changes changed a composed sheet")
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
	if !(0 <= from && from < mid && mid < to) {
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

func TestLayerStatementIsRenderedAheadOfEverythingItOrders(t *testing.T) {
	t.Parallel()
	s := css.NewSheet().Select(".a", css.Decl("color", css.Literal("red")))
	s.LayerOrder("tokens", "client")
	want := "@layer tokens, client;\n\n.a {\n  color: red;\n}"
	if got := s.CSS(); got != want {
		t.Fatalf("a statement added after the rules still precedes them:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestLayerBlockNestsAndIndents(t *testing.T) {
	t.Parallel()
	s := css.NewSheet()
	s.Layer("client", func(inner *css.Sheet) {
		inner.Select(".a", css.Decl("display", css.Literal("flex")))
	})
	want := "@layer client {\n  .a {\n    display: flex;\n  }\n}"
	if got := s.CSS(); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestLayerNamesRefuseAnEscape(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"Client", "tokens base", "1token", "", "tok; }"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Layer(%q) did not panic", bad)
				}
			}()
			css.NewSheet().Layer(bad, func(*css.Sheet) {})
		}()
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("LayerOrder(%q) did not panic", bad)
				}
			}()
			css.NewSheet().LayerOrder("tokens", bad)
		}()
	}
}

func TestUsesLayersReadsThroughNestedAtRules(t *testing.T) {
	t.Parallel()
	plain := css.NewSheet().Media("(min-width: 40rem)", func(in *css.Sheet) {
		in.Select(".a", css.Decl("color", css.Literal("red")))
	})
	nested := css.NewSheet().Media("(min-width: 40rem)", func(in *css.Sheet) {
		in.Layer("client", func(in2 *css.Sheet) { in2.Select(".a", css.Decl("color", css.Literal("red"))) })
	})
	statement := css.NewSheet().LayerOrder("tokens")
	if plain.UsesLayers() || !nested.UsesLayers() || !statement.UsesLayers() {
		t.Fatal("UsesLayers missed a @layer or imagined one")
	}
}

func TestWalkRulesVisitsNestedRulesInOrder(t *testing.T) {
	t.Parallel()
	s := css.NewSheet().Select(".a", css.Decl("color", css.Literal("red")))
	s.Media("(min-width: 40rem)", func(in *css.Sheet) {
		in.Select(".b", css.Decl("color", css.Literal("blue")))
	})
	s.Keyframes("pulse", func(k *css.Keyframes) {
		k.At("from", css.Decl("opacity", css.Literal("0")))
	})
	var seen []string
	if err := s.WalkRules(func(sel string, _ []css.Declaration) error {
		seen = append(seen, sel)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A keyframe stop is a rule the browser applies to the animating element, so
	// a caller that reads the sheet reads it too, with the offset as its selector.
	if len(seen) != 3 || seen[0] != ".a" || seen[1] != ".b" || seen[2] != "from" {
		t.Fatalf("walked %v", seen)
	}
}

// TestVerbatimVisitsEveryWordTheSheetWrites pins the walk a caller needs before
// it places someone else's sheet inside a block: the emitted text of a rule is
// the selector, each property and each rendered value, and the text before a
// block — an at-rule prelude — is written verbatim too, so a reader of the rules
// alone would miss the one place a client can put a brace outside a declaration.
func TestVerbatimVisitsEveryWordTheSheetWrites(t *testing.T) {
	t.Parallel()
	s := css.NewSheet().Select(".a", css.Decl("color", css.VarRef("pk-color-fg-primary", "")))
	s.Media("(min-width: 40rem)", func(in *css.Sheet) {
		in.Select(".b", css.Decl("opacity", css.Literal("0.5")))
	})
	s.Keyframes("pulse", func(k *css.Keyframes) {
		k.At("from", css.Decl("opacity", css.Literal("0")))
	})
	var seen []string
	if err := s.Verbatim(func(text string) error {
		seen = append(seen, text)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{".a", "color", "var(--pk-color-fg-primary)", "@media (min-width: 40rem)",
		".b", "opacity", "0.5", "@keyframes pulse", "from", "opacity", "0"}
	for _, w := range want {
		if !slices.Contains(seen, w) {
			t.Errorf("Verbatim did not visit %q; it visited %v", w, seen)
		}
	}
	// The walk stops on the first refusal, which is how ui.Compose reports one.
	visits := 0
	err := s.Verbatim(func(string) error {
		visits++
		return errors.New("refused")
	})
	if err == nil || visits != 1 {
		t.Fatalf("Verbatim visited %d words and returned %v", visits, err)
	}
}

// TestHeadsVisitsTheTextAheadOfEveryBraceTheEmitterWrites pins the one thing that
// separates Heads from Verbatim: it reports every text the emitter writes before a
// `{` it supplies, in emission order and at any depth, and no text between the
// braces. The at-keyword of a prelude is the emitter's, so a @media query reaches
// the caller as the query it was handed and a @keyframes name as the name — the
// `@` is not the caller's text, and a read that reported it would refuse every
// at-rule a consumer is told to write.
func TestHeadsVisitsTheTextAheadOfEveryBraceTheEmitterWrites(t *testing.T) {
	t.Parallel()
	s := css.NewSheet().Select(".a", css.Decl("color", css.Literal("red")))
	s.Media("(min-width: 40rem)", func(in *css.Sheet) {
		in.Select(".b", css.Decl("opacity", css.Literal("0.5")))
	})
	s.Keyframes("pulse", func(k *css.Keyframes) {
		k.At("from", css.Decl("opacity", css.Literal("0")))
	})
	var seen []string
	if err := s.Heads(func(text string) error {
		seen = append(seen, text)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{".a", "(min-width: 40rem)", ".b", "pulse", "from"}; !slices.Equal(seen, want) {
		t.Fatalf("Heads visited %v, want %v in emission order", seen, want)
	}
	visits := 0
	if err := s.Heads(func(string) error { visits++; return errors.New("refused") }); err == nil || visits != 1 {
		t.Fatalf("Heads visited %d heads and returned %v", visits, err)
	}
}
