package richtext

import (
	"errors"
	"strings"
	"testing"
)

// Decision 0069 §1 bans TeX math and emoji shortcodes. It bans the construct,
// not the two characters that happen to delimit it: an author writing a price or
// a label writes "$5 and $8" and "note:todo:" without writing math or an emoji,
// and the 422 they got named something they never wrote. The two sentences the
// audit named are stored exactly as their author typed them.
func TestProseThatOnlyLooksLikeMathOrAShortcodeIsStoredAsWritten(t *testing.T) {
	for _, source := range []string{
		"Tickets cost $5 and $8.\n",
		"see note:todo: later\n",
		"The licence is $1,299.99 a year, between $5 and $8 per seat.\n",
		"Invoice: paid $4,000: net $3,600: tax $400\n",
		"Opens 9:30: the label note:todo: stays until 10:15\n",
		"Both at once: seats are $5, see note:todo: for the rest.\n",
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

// What the ban is for stays banned, with the same construct name, the same
// message key and the same line the author has to go back and change.
func TestTeXMathAndEmojiShortcodesKeepTheirRefusal(t *testing.T) {
	for _, tc := range []struct {
		source    string
		construct string
		key       string
		line      int
	}{
		{"A formula $a^b$ here\n", "math", "math", 1},
		{"A formula $x^{2}$ here\n", "math", "math", 1},
		{"A formula $x_{i}$ here\n", "math", "math", 1},
		{"A formula $\\alpha$ here\n", "math", "math", 1},
		{"A formula $^2$ here\n", "math", "math", 1},
		{"## Title\n\nA formula $a^b$ here\n", "math", "math", 3},
		{"Tickets cost $5 and $8, and $a^b$ holds.\n", "math", "math", 1},
		{"A block $$a$$ here\n", "math", "math", 1},
		{"A block $${x}$$ here\n", "math", "math", 1},
		{"## Title\n\nA block $$a$$ here\n", "math", "math", 3},
		{":smile:\n", "emoji shortcode", "emoji-shortcode", 1},
		{"A line :smile: here\n", "emoji shortcode", "emoji-shortcode", 1},
		{"A line :thumbs_up: here\n", "emoji shortcode", "emoji-shortcode", 1},
		{"## Title\n\nA line :smile: here\n", "emoji shortcode", "emoji-shortcode", 3},
		{"see note:todo: later, and :smile:\n", "emoji shortcode", "emoji-shortcode", 1},
	} {
		t.Run(strings.TrimSuffix(tc.source, "\n"), func(t *testing.T) {
			_, err := Normalise(tc.source)
			var refused *Refused
			if !errors.As(err, &refused) || len(refused.Issues) != 1 {
				t.Fatalf("%q: got %v, want one refusal", tc.source, err)
			}
			issue := refused.Issues[0]
			if issue.Construct != tc.construct || issue.Key() != tc.key || issue.Line != tc.line {
				t.Errorf("%q: refused as %q (key %q) on line %d, want %q (key %q) on line %d",
					tc.source, issue.Construct, issue.Key(), issue.Line, tc.construct, tc.key, tc.line)
			}
		})
	}
}

// The masking of literal code is the same door it was: a TeX mark inside a code
// span or a fenced block stays the code its author wrote, and the same text in
// prose is refused. A price in prose is accepted either way.
func TestATexMarkInCodeIsStillCodeAndInProseIsStillRefused(t *testing.T) {
	for _, source := range []string{"Run `a_b $x^2$` and pay $5.\n", "```go\nx := $a^b$\n```\n", "    cost = $5 + $8\n"} {
		t.Run(strings.TrimSuffix(source, "\n"), func(t *testing.T) {
			if _, err := Normalise(source); err != nil {
				t.Errorf("code was refused: %v", err)
			}
		})
	}
	if _, err := Normalise("A formula $x^2$ in prose\n"); err == nil {
		t.Error("a formula in prose was accepted")
	}
}
