package components

import (
	"strings"
	"testing"
)

func TestAFullWidthFieldBoundsItsHelpSentence(t *testing.T) {
	t.Parallel()
	out := draw(t, Input(InputProps{
		Name: "assigneeId", Label: "Assignee", FullWidth: true,
		HelpText: "The user who becomes responsible",
	}))
	paragraph := strings.Index(out, `id="pk-input-assigneeId-help"`)
	if paragraph < 0 {
		t.Fatalf("the field draws no help paragraph:\n%s", out)
	}
	start := strings.LastIndex(out[:paragraph], "<p")
	end := strings.Index(out[paragraph:], ">")
	if start < 0 || end < 0 {
		t.Fatalf("the help paragraph has no opening tag:\n%s", out)
	}
	if tag := out[start : paragraph+end+1]; !strings.Contains(tag, "max-w-") {
		t.Errorf("a full-width control left its help sentence unbounded: %s", tag)
	}
}
