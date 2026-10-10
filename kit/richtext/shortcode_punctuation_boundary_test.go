package richtext

import (
	"errors"
	"strings"
	"testing"
)

// RE2 has no lookbehind, so the shortcode's word boundary is written as a
// consumed character on each side. The fragile half of that construction is the
// boundary that is punctuation rather than start or end of line: a shortcode an
// author brackets, quotes or hyphenates sits between two consumed characters and
// must still be the construct the ban names, on its own line.
func TestAShortcodeHeldByPunctuationIsStillRefused(t *testing.T) {
	for _, tc := range []struct {
		source string
		line   int
	}{
		{"(:smile:)\n", 1},
		{"\":smile:\"\n", 1},
		{"x-:smile:-y\n", 1},
		{"## Title\n\nwe shipped (:tada:) today\n", 3},
	} {
		t.Run(strings.TrimSuffix(tc.source, "\n"), func(t *testing.T) {
			_, err := Normalise(tc.source)
			var refused *Refused
			if !errors.As(err, &refused) || len(refused.Issues) != 1 {
				t.Fatalf("%q: got %v, want one refusal", tc.source, err)
			}
			issue := refused.Issues[0]
			if issue.Construct != "emoji shortcode" || issue.Key() != "emoji-shortcode" || issue.Line != tc.line {
				t.Errorf("%q: refused as %q (key %q) on line %d, want emoji shortcode (key emoji-shortcode) on line %d",
					tc.source, issue.Construct, issue.Key(), issue.Line, tc.line)
			}
		})
	}
}

// The other side of the same boundary: a colon run inside a word, a time or a
// trailing label is prose, however shortcode-shaped its letters are.
func TestAColonInsideAWordOrATimeStaysProse(t *testing.T) {
	for _, source := range []string{
		"9:30:15 elapsed\n",
		"release:candidate: builds are tagged\n",
		"the ratio is 3:2:1 overall\n",
	} {
		t.Run(strings.TrimSuffix(source, "\n"), func(t *testing.T) {
			stored, err := Normalise(source)
			if err != nil {
				t.Fatalf("%q was refused: %v", source, err)
			}
			if stored != source {
				t.Errorf("%q was stored as %q, want it stored as written", source, stored)
			}
		})
	}
}
