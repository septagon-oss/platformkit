package forms

// Two columns of the editor's command table have to stay in step, and nothing but
// this file reads them together: a command that gained a keystroke without a name
// message would be a keystroke nobody is ever told, and a name message that gained
// a value it does not ask for — or lost the one it asked for — reaches a person as
// Go's verbs or as an empty parenthesis. The catalogues are checked elsewhere for
// carrying every key with the same arguments; the table itself is checked here.

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// verbForm is one argument slot spelled for fmt: `%[1]s`, or the bare `%s` a
// caller could still answer positionally.
var verbForm = regexp.MustCompile(`%(\[\d+\])?[sdq]`)

// TestEveryCommandThatNamesAKeystrokeNamesItInOnePhrase walks the editor's own
// table: a command has a keystroke exactly when it has a name message, that
// message's source text asks for exactly one value, and the value the renderer
// passes is the keystroke itself. A command with no keystroke names nothing, so
// its source text must ask for nothing — a fallback that asks while nothing is
// passed arrives as "Bold ()".
func TestEveryCommandThatNamesAKeystrokeNamesItInOnePhrase(t *testing.T) {
	for _, c := range richtextCommands {
		verbs := len(verbForm.FindAllString(c.Name, -1))
		switch {
		case c.Shortcut == "" && c.NameKey != "":
			t.Errorf("%s: no keystroke but the name %q is asked of %q, which passes nothing to interpolate",
				c.Command, c.Name, c.NameKey)
		case c.Shortcut == "" && verbs != 0:
			t.Errorf("%s: no keystroke, yet the name %q asks for %d values, which renders as an empty parenthesis",
				c.Command, c.Name, verbs)
		case c.Shortcut != "" && c.NameKey == "":
			t.Errorf("%s: the keystroke %s belongs to no name message, so nothing ever tells a person it exists",
				c.Command, c.Shortcut)
		case c.Shortcut != "" && verbs != 1:
			t.Errorf("%s: the name %q asks for %d values where one keystroke %s was declared",
				c.Command, c.Name, verbs, c.Shortcut)
		}
		if c.Shortcut == "" {
			continue
		}
		// The source text is what a render with no catalogue reads, so it has to
		// land as the sentence itself: the keystroke inside it, and no verb left.
		spoken := fmt.Sprintf(c.Name, c.Shortcut)
		if !strings.Contains(spoken, c.Shortcut) || strings.Contains(spoken, "%!") {
			t.Errorf("%s: %q with the keystroke %s in it reads as %q", c.Command, c.Name, c.Shortcut, spoken)
		}
	}
}

// TestNoEditorCommandNameReachesAPersonWithGoVerbsInIt draws the whole editor the
// way a caller that composed no seam does, and refuses any verb or empty
// parenthesis on the page: every name this package draws without a formatter is
// the sentence it was written in, finished, because the phrase and its value are
// one message.
func TestNoEditorCommandNameReachesAPersonWithGoVerbsInIt(t *testing.T) {
	var out strings.Builder
	if err := Control(ControlProps{
		Field: Field{Definition: entity.Field{
			Name: "body", Type: entity.TypeText, Widget: "richtext", Doc: "The published text.",
		}, Label: "Body"},
		ID: "f",
	}).Render(&out); err != nil {
		t.Fatal(err)
	}
	page := out.String()
	for _, forbidden := range []string{"%!", "%[", "()"} {
		if strings.Contains(page, forbidden) {
			t.Errorf("the editor draws %q where a person reads a command name: %s", forbidden, page)
		}
	}
	if got := strings.Count(page, "Ctrl+"); got == 0 {
		t.Errorf("the editor drew no keystroke into any command name, so its shortcuts went unannounced: %s", page)
	}
}
