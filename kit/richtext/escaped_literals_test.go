package richtext

import "testing"

// The escape is the door an author who means one of these characters as literal
// text has always had, and the narrowing of the two prose-shaped matchers leaves
// it exactly as it was: the refusal table never sees the escaped character, and
// the page shows the text with no backslash in it. The $ row is the one this
// change is about — an author escaping a price writes it the way they always did.
func TestEscapedUnsupportedSyntaxRemainsLiteralText(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{`\:smile:`, "<p>:smile:</p>\n"},
		{`\[^note]`, "<p>[^note]</p>\n"},
		{`Costs \$5 and \$8.`, "<p>Costs $5 and $8.</p>\n"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			stored, err := Normalise(tc.source)
			if err != nil {
				t.Fatalf("escaped text was refused as syntax: %v", err)
			}
			rendered, err := RenderLegacy(stored)
			if err != nil {
				t.Fatalf("escaped text did not render: %v", err)
			}
			if rendered != tc.want {
				t.Errorf("escaped text rendered %q, want %q", rendered, tc.want)
			}
		})
	}
}
