package ui_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
)

// guidedRule is one rule the guided screen's own styling wrote: a selector that
// names a data-guided hook, beside the declarations it gave it.
var guidedRule = regexp.MustCompile(`[^{\s]*\[[^\]]*data-guided[^\]]*\][^{]*\{([^}]*)\}`)

// TestTheGuidedScreenCapsWhatItWritesToTheScreen checks the one measure the
// design floor refuses on every guided screen: the floor keeps a body line under
// 75 characters, and the block a session writes its prose into is capped at
// 72rem with nothing inside it narrowing the text, so at a desktop width the
// step's instruction, its item lines and the summary sentences run the full
// 1152px. A cap of its own on the prose — in ch, or a width no wider than the
// ~40rem 75 characters of body text measure — is what clears it.
//
// The two left-edge refusals and the third body size the floor counted are not
// asserted here: one depends on the shell a client composes these screens into,
// and the other is a type role rather than a cap.
func TestTheGuidedScreenCapsWhatItWritesToTheScreen(t *testing.T) {
	sheet := string(ui.Compose(design.Default()).Body)
	if !strings.Contains(sheet, "data-guided") {
		t.Fatal("the composed sheet carries no guided rules at all, so this case measures nothing")
	}
	for _, rule := range guidedRule.FindAllStringSubmatch(sheet, -1) {
		for _, decl := range strings.Split(rule[1], ";") {
			name, value, found := strings.Cut(decl, ":")
			if !found {
				continue
			}
			name, value = strings.TrimSpace(name), strings.TrimSpace(value)
			if name != "max-inline-size" && name != "max-width" {
				continue
			}
			number, unit := strings.TrimRight(value, "abcdefghijklmnopqrstuvwxyz"), strings.TrimLeft(value, "0123456789.")
			n, err := strconv.ParseFloat(number, 64)
			if err != nil {
				continue
			}
			if unit == "ch" && n <= 75 || unit == "rem" && n <= 40 {
				return // the prose is capped within a readable measure
			}
		}
	}
	t.Errorf("the guided rules cap the screen at 72rem and the prose at nothing: a body line runs past 75 characters")
}
