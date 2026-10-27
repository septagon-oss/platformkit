package view_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAnIntegerAtTheNegativeEdgeOfTheSafeDomainKeepsItsDigits reads the value domain at
// the one magnitude where the two signs are not the same number: a Page writes a JSON
// number for |v| <= 9007199254740991 and a decimal string of the same digits for
// |v| >= 9007199254740992, so the first value past the bound below zero,
// -9007199254740992, lies one step further from zero than the bound it crosses. A build
// that let one more digit through on the negative side alone would keep this value a JSON
// number — a double does hold 2^53 exactly, which is the reasoning behind that mistake —
// while every other magnitude reads the same, and the digits a shell whose only number
// type is a double must not round would reach it unnoticed. The case therefore asks for
// the digits as the page spells them, quoted, that being the spelling a phone and a
// terminal both read.
func TestAnIntegerAtTheNegativeEdgeOfTheSafeDomainKeepsItsDigits(t *testing.T) {
	t.Parallel()

	page := build(t, domainInput(map[string]any{
		"id": "t1", "title": "n", "count": json.Number("-9007199254740992"),
	}, time.UTC))
	var edge view.Field
	for _, section := range page.Sections {
		for _, field := range section.Fields {
			if field.Name == "count" {
				edge = field
			}
		}
	}
	if edge.Name != "count" {
		t.Fatal("the count field is not on the page, so this case would inspect nothing")
	}
	if edge.ValueKind != view.ValueBigInteger || edge.Value != "-9007199254740992" {
		t.Errorf("count -9007199254740992 is %s %v, want the big integer of the same digits", edge.ValueKind, edge.Value)
	}
	if raw := string(bytes(t, page)); !strings.Contains(raw, `"-9007199254740992"`) {
		t.Errorf("the page spells the negative edge somewhere other than a quoted string of its own digits:\n%s", raw)
	}
}
