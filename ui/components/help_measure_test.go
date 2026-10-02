package components

// A field's help sentence is body copy the design floor measures: the floor
// refuses any <p> wider than 75 characters, and an unbounded help line under a
// full-width control on a generated record page measured 189ch at 1440px — the
// one refusal the frame's own floor case still prints as KNOWN. The bound may sit
// on the sentence or on the field that holds it; it must sit somewhere between
// the control's own markup and the paragraph.

import (
	"strings"
	"testing"
)

func TestAFieldsHelpSentenceIsBounded(t *testing.T) {
	t.Parallel()
	out := draw(t, Input(InputProps{Name: "assigneeId", Label: "Assignee", HelpText: "The user who becomes responsible"}))
	help := strings.Index(out, `-help"`)
	if help < 0 {
		t.Fatalf("the field draws no help paragraph:\n%s", out)
	}
	if !strings.Contains(out[:help+strings.Index(out[help:], ">")], "max-w-") {
		t.Errorf("neither the help paragraph nor the field around it carries a measure bound:\n%s", out)
	}
}
