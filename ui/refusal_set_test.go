package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/css"
)

// What a mounted composition refuses, and what the design capture accepts.
//
// The claim has two halves. The tool half — a design capture has to be able to
// render a sheet a page would be refused — is pinned by compose_design_test.go on two rules.
// The mounted half, the sentence in the commit body that says `Compose` "still panics on
// every one of those spellings", had no case: the sibling files each check one
// vocabulary (kernel attributes, kernel classes, the colour read's
// bounds), and none named the spellings that actually broke CI's browser step. That
// matters because the two compositions now part company: a refusal that stops firing is
// invisible on the tool side, where nothing is refused by design, and shows up only as a
// rule riding into the sheet a page links.
//
// So each case below asserts the refusal and that the refusal names the key a developer has
// to find — the attribute name or the colour — and then that ComposeDesign places the same
// rule instead of refusing it. The last case pins the claim in ComposeDesign's own doc
// comment, which compose_design_test.go's byte comparison does not: the exported sheet
// carries the fingerprint the served one would.
func composedRefusal(t *testing.T, compose func(design.Pair, ...Extra) Sheet, rule *css.Sheet) (text string) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			text = fmt.Sprint(recovered)
		}
	}()
	compose(design.Default(), Extra{Sheets: []*css.Sheet{rule}})
	return ""
}

func TestMountedCompositionRefusesWhatTheDesignFixturesCarry(t *testing.T) {
	cases := []struct {
		name string
		rule *css.Sheet
		// key is what the refusal has to name: the browser's canonical name for the
		// thing the rule takes, not the sentence around it.
		key string
	}{
		{"margin on a proposed alert's icon",
			css.NewSheet().Select("[data-alert-icon]", css.Decl("margin-top", css.Literal("2px"))),
			"data-alert-icon"},
		{"a rule beside the button component",
			css.NewSheet().Select("[data-component=button]", css.Decl("margin", css.Literal("1px"))),
			"data-component"},
		{"a background on the card component",
			css.NewSheet().Select("[data-component=card]", css.Decl("background-color", css.Literal("#eee"))),
			"data-component"},
		{"a three-digit authored colour",
			css.NewSheet().Select(".store-panel", css.Decl("background-color", css.Literal("#eee"))),
			"#eee"},
		{"a six-digit authored colour",
			css.NewSheet().Select(".store-panel", css.Decl("color", css.Literal("#123456"))),
			"#123456"},
		{"an authored colour in functional notation",
			css.NewSheet().Select(".store-panel", css.Decl("color", css.Literal("rgba(1,2,3,0.5)"))),
			"rgba("},
		{"an authored colour inside a shadow",
			css.NewSheet().Select(".store-panel", css.Decl("box-shadow", css.Literal("0 1px 2px rgba(0,0,0,0.2)"))),
			"rgba("},
	}
	for _, tc := range cases {
		refused := composedRefusal(t, Compose, tc.rule)
		if refused == "" {
			t.Errorf("%s: Compose accepted %s — the sheet a page links carries a rule the gate exists to refuse, and ComposeDesign refusing it by design would hide it",
				tc.name, ruleText(tc.rule))
			continue
		}
		if !strings.Contains(refused, tc.key) {
			t.Errorf("%s: Compose refused the rule but its message names neither %q nor the rule: %s", tc.name, tc.key, refused)
		}
		if snapshot := composedRefusal(t, ComposeDesign, tc.rule); snapshot != "" {
			t.Errorf("%s: ComposeDesign refused %s (%s) — the capture tool exists to render the sheet a mount refuses, and refusing it there is the bug a93e226 fixed",
				tc.name, ruleText(tc.rule), snapshot)
		}
	}
}

// A consumer rule that reads a token stays legal on both sides of the seam — the half
// that keeps ComposeDesign from becoming "the sheet with no rules about sheets".
func TestBothCompositionsStillAcceptATokenAndAgreeOnItsBytes(t *testing.T) {
	rule := css.NewSheet().Select(".store-panel",
		css.Decl("margin-top", css.Literal("2px")),
		css.Decl("color", css.VarRef("pk-color-accent-default", "")))
	if refused := composedRefusal(t, Compose, rule); refused != "" {
		t.Fatalf("Compose refused a rule that names nothing but its own class and reads a token: %s", refused)
	}
	if snapshot := composedRefusal(t, ComposeDesign, rule); snapshot != "" {
		t.Fatalf("ComposeDesign refused the same legal rule: %s", snapshot)
	}
	served, exported := Compose(design.Default(), Extra{Sheets: []*css.Sheet{rule}}),
		ComposeDesign(design.Default(), Extra{Sheets: []*css.Sheet{rule}})
	if string(served.Body) != string(exported.Body) {
		t.Error("the two compositions disagree about the bytes of a sheet both accept")
	}
	if served.Fingerprint != exported.Fingerprint || served.Fingerprint == "" {
		t.Errorf("ComposeDesign's fingerprint is %q and Compose's is %q: ComposeDesign's doc comment promises the exported fingerprint is computed the same way as the served one, and a snapshot whose fingerprint is not the sheet's is a fact nobody can check against a page",
			exported.Fingerprint, served.Fingerprint)
	}
}

func ruleText(sheet *css.Sheet) string {
	var out strings.Builder
	_ = sheet.WalkRules(func(selector string, decls []css.Declaration) error {
		out.WriteString(selector)
		for _, d := range decls {
			out.WriteString(" " + d.CSS())
		}
		return nil
	})
	return out.String()
}
