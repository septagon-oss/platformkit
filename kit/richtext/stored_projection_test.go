package richtext

import (
	"strings"
	"testing"
)

// storedRound checks what the reader is shown and that storing is a fixed point.
// This asks about the other projection: the plain text a page description, a
// search snippet and a notification are cut from. Turning prose into a code
// block moves words out of it even where the rendered document is compared
// elsewhere, so the words are pinned here for the shapes the normaliser rewrites.
func TestStoringKeepsTheWordsAReaderReads(t *testing.T) {
	words := func(s string) string {
		d, err := Parse(s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return strings.Join(strings.Fields(PlainText(d)), " ")
	}
	for _, source := range []string{
		"\tcode\n\tmore",
		"Intro\n\n   \tcode",
		"Intro\n\n     code\n     more",
		"- a\n\n\t\tdeep",
		"> quoted\n>\n>     code",
		"Intro\n\n    ```go\n    x\n    ```",
		"Intro\n\n    a\n\n    b",
		"Intro\n\n    1. not a list\n    2. still code",
		"| a | b |\n|---|---|\n| 1 | 2 |\n\n    code",
		"Intro\n\n    é\n    ü",
		"1. Open the door\n\n    Then walk in.\n\n2. Sit down",
		"- fruit\n\n    - apple\n    - pear",
		"text\n\n    code\n\n\n    more",
		"## h\n\n- item\n\n  continued",
	} {
		t.Run(source, func(t *testing.T) {
			_, _, stored := storedRound(t, source)
			if got, want := words(stored), words(source); got != want {
				t.Errorf("storing moved words in or out of the plain text a page description is cut from:\n source %q\n stored %q", want, got)
			}
		})
	}
}
