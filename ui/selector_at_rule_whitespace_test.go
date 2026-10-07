package ui_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// Leading CSS whitespace cannot disguise an at-rule as a selector, while the
// same text in a quoted attribute value remains data in the client layer.
func TestSelectorAtRulesAreRefusedAfterWhitespaceButAllowedAsAttributeData(t *testing.T) {
	for _, prefix := range []string{"", " ", "\t", "\n", "\r", "\f", " \t\n\r\f"} {
		t.Run(prefix, func(t *testing.T) {
			for _, rule := range []string{"@layer client", "@layer tokens", "@media all"} {
				selector := prefix + rule
				func() {
					defer func() {
						if recover() == nil {
							t.Errorf("Compose accepted at-rule selector %q", selector)
						}
					}()
					sheet := css.NewSheet().Select(selector, css.Decl("color", css.Literal("currentColor")))
					ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}})
				}()
				dataSelector := `[data-client-text="` + rule + `"]`
				sheet := css.NewSheet().Select(prefix+dataSelector, css.Decl("color", css.Literal("currentColor")))
				body := string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}}).Body)
				if !strings.Contains(body, dataSelector) || layerHoldingSelector(t, body, dataSelector) != "client" {
					t.Errorf("quoted at-rule data %q was not retained inside the client layer", dataSelector)
				}
			}
		})
	}
}
