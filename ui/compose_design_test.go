package ui

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/css"
)

// Compose is the sheet a page links and refuses what a page may not carry;
// ComposeDesign is the sheet ui/export renders and refuses nothing. The two must
// differ in the refusals alone, or the seam is a second stylesheet builder and the
// snapshot stops predicting the page. These cases pin both halves: the sheet
// Compose refuses is still placed by ComposeDesign in @layer client under the same
// order statement, and a sheet Compose accepts composes to the same bytes either
// way.
func TestComposeDesignPlacesWhatComposeRefusesAndNothingElseDifferently(t *testing.T) {
	refused := css.NewSheet().
		Select("[data-component=card]", css.Decl("background-color", css.Literal("#eee"))).
		Select("[data-alert-icon]", css.Decl("margin-top", css.Literal("2px")))
	func() {
		defer func() {
			if recover() == nil {
				t.Error("Compose accepted a client sheet that names a kernel attribute and carries a raw colour: the refusals belong to the sheet a page links, and ComposeDesign must not take them away from Compose")
			}
		}()
		Compose(design.Default(), Extra{Sheets: []*css.Sheet{refused}})
	}()

	sheet := ComposeDesign(design.Default(), Extra{Sheets: []*css.Sheet{refused}})
	text := string(sheet.Body)
	client := text[strings.Index(text, "@layer client {"):]
	for _, want := range []string{
		"@layer tokens, base, components, client;",
		"@layer tokens {", "@layer base {", "@layer components {", "@layer client {",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("ComposeDesign emitted no %q; the snapshot sheet has to be the same four layers with the consumer's rules in the client layer, or the .fig export renders a different document from the one the page would render", want)
		}
	}
	for _, want := range []string{
		"[data-component=card] {", "background-color: #eee;",
		"[data-alert-icon] {", "margin-top: 2px;",
	} {
		if !strings.Contains(client, want) {
			t.Errorf("the @layer client block ComposeDesign emitted carries no %q: the refused rules have to land in the one layer a consumer writes, in the sheet the export renders", want)
		}
	}
	if strings.Count(text, "@layer ") != 5 {
		t.Errorf("ComposeDesign emitted %d @layer statements rather than the order statement and four blocks: %s", strings.Count(text, "@layer "), text)
	}

	accepted := css.NewSheet().Select(".store-card", css.Decl("margin-top", css.Literal("2px")))
	if legal, snapshot := string(Compose(design.Default(), Extra{Sheets: []*css.Sheet{accepted}}).Body),
		string(ComposeDesign(design.Default(), Extra{Sheets: []*css.Sheet{accepted}}).Body); legal != snapshot {
		t.Error("a sheet Compose accepts composes differently under ComposeDesign: the seam would then be a second composition, not the same composition without the page's refusals")
	}
	if string(Compose(design.Default()).Body) != string(ComposeDesign(design.Default()).Body) {
		t.Error("Compose and ComposeDesign disagree about a composition with no consumer sheet at all")
	}
}
