package richtext

import (
	"errors"
	"strings"
	"testing"
)

const testImage = "pk-file:550e8400-e29b-41d4-a716-446655440000"

func TestDecision0069FormatRows(t *testing.T) {
	cases := []struct{ name, allowed, refused, construct string }{
		{"paragraphs and breaks", "one\n\ntwo\\\nthree", "one\n<script>alert(1)</script>", "raw HTML"},
		{"headings", "## Opening\n### Times\n#### Friday", "# Title", "heading level 1"},
		{"inline marks", "**bold** *italic* ~~strike~~ `code`", "[^note]", "footnote"},
		{"links", "[site](/help) [web](https://example.test)", "[bad](javascript:alert(1))", "link destination"},
		{"lists", "- first\n  - second\n    - third", "- first\n  - second\n    - third\n      - fourth", "list nesting"},
		{"quotes code and rule", "> Quote\n\n```go\nx\n```\n\n---", "    code", ""},
		{"tables", "| A | B |\n| --- | --- |\n| x | y |", "| A |\n| --- |\n| ![x](" + testImage + ") |", "image in table"},
		{"block image", "![Alt](" + testImage + ` "Caption")`, "![x](https://example.test/x.png)", "image source"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			normal, err := Normalise(tc.allowed)
			if err != nil {
				t.Fatalf("allowed: %v", err)
			}
			again, err := Normalise(normal)
			if err != nil || again != normal {
				t.Fatalf("not idempotent: %q then %q, %v", normal, again, err)
			}
			if tc.construct == "" {
				normal, err = Normalise(tc.refused)
				if err != nil || !strings.HasPrefix(normal, "```") {
					t.Fatalf("indented code: %q, %v", normal, err)
				}
				return
			}
			_, err = Normalise(tc.refused)
			var refused *Refused
			if !errors.As(err, &refused) {
				t.Fatalf("wanted refusal: %v", err)
			}
			found := false
			for _, issue := range refused.Issues {
				if issue.Construct == tc.construct && issue.Line > 0 && issue.Remedy != "" {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s with line/remedy: %+v", tc.construct, refused.Issues)
			}
		})
	}
}

func TestRefusalsCarrySubmittedLinesAndRemedies(t *testing.T) {
	_, err := Normalise("okay\n<script>x</script>\n![x](https://example.test/x.png)\n# Title")
	var refused *Refused
	if !errors.As(err, &refused) {
		t.Fatalf("wanted refusal: %v", err)
	}
	want := map[string]int{"raw HTML": 2, "image source": 3, "heading level 1": 4}
	for _, issue := range refused.Issues {
		if line, ok := want[issue.Construct]; ok {
			if issue.Line != line || issue.Remedy == "" {
				t.Errorf("%s: %+v, want line %d", issue.Construct, issue, line)
			}
			delete(want, issue.Construct)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing refusals: %v", want)
	}
}

func FuzzNormaliseIdempotent(f *testing.F) {
	for _, source := range []string{"", "plain", "## Heading", "**bold** *italic*", "- one\n- two", "    code", "| A |\n| --- |\n| b |", "![x](" + testImage + ")", "<script>x</script>"} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		one, err := Normalise(source)
		if err != nil {
			return
		}
		two, err := Normalise(one)
		if err != nil || one != two {
			t.Fatalf("normalise(%q)=%q; again=%q: %v", source, one, two, err)
		}
	})
}
