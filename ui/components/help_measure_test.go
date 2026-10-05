package components

// A field's help sentence is body copy the design floor measures: the floor
// refuses any <p> wider than 75 characters, and an unbounded help line under a
// full-width control on a generated record page measured 189ch at 1440px. The
// bound sits on the sentence itself. It used to be allowed to sit on the field
// around it, which is the case this file used to read out of a prefix of the
// markup — a field's flex column is now the one element that must carry no bound
// of its own, because the design tool projects no composition whose own sizing is
// constrained (see clFieldWrap), and a paragraph's bound is what both the floor
// and the projection read. The full-width field, the shape the record page draws,
// is pinned in full_width_help_measure_test.go.

import (
	"strings"
	"testing"
)

func TestAFieldsHelpSentenceIsBounded(t *testing.T) {
	t.Parallel()
	out := draw(t, Input(InputProps{Name: "assigneeId", Label: "Assignee", HelpText: "The user who becomes responsible"}))
	help := strings.Index(out, `id="pk-input-assigneeId-help"`)
	if help < 0 {
		t.Fatalf("the field draws no help paragraph:\n%s", out)
	}
	start := strings.LastIndex(out[:help], "<p")
	end := strings.Index(out[help:], ">")
	if start < 0 || end < 0 {
		t.Fatalf("the help paragraph has no opening tag:\n%s", out)
	}
	if paragraph := out[start : help+end+1]; !strings.Contains(paragraph, "max-w-") {
		t.Errorf("the help paragraph carries no measure bound: %s", paragraph)
	}
}
