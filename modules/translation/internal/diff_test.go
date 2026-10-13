package internal

import (
	"strings"
	"testing"
)

// TestAParagraphEditChangesOneParagraphOnBothSides is journey 2's highlight:
// edit one paragraph of the source and the translate view has to light up that
// paragraph and only that paragraph, on both panes.
func TestAParagraphEditChangesOneParagraphOnBothSides(t *testing.T) {
	before := "We build software.\n\nOur office is in Lisbon.\n\nWe hire engineers."
	after := "We build software for banks.\n\nOur office is in Lisbon.\n\nWe hire engineers."

	diff, err := DiffSource(before, after, false)
	if err != nil {
		t.Fatalf("DiffSource: %v", err)
	}
	if diff.All {
		t.Fatal("the diff refused a three-paragraph field")
	}
	var changed []Change
	unchanged := 0
	for _, c := range diff.Changes {
		if c.Kind == Changed {
			changed = append(changed, c)
		} else {
			unchanged++
		}
	}
	if len(changed) != 1 {
		t.Fatalf("the diff marked %d paragraphs changed, want the one that moved: %v", len(changed), diff.Changes)
	}
	if changed[0].SourceIndex != 0 || changed[0].TargetIndex != 0 {
		t.Errorf("the changed pair is source %d against target %d, want 0 against 0",
			changed[0].SourceIndex, changed[0].TargetIndex)
	}
	if unchanged != 2 {
		t.Errorf("the diff found %d unchanged paragraphs, want the two that did not move", unchanged)
	}
}

// TestADiffOfATextAgainstItselfFindsNothing is C21, the property the whole
// highlight rests on: a field is never shown as having changed paragraphs when
// its source is the text the translation was made from.
func TestADiffOfATextAgainstItselfFindsNothing(t *testing.T) {
	texts := []string{
		"",
		"One paragraph.",
		"Two.\n\nParagraphs here.",
		"## Heading\n\nA list:\n\n* one\n* two\n\nFinal word.",
		"```go\nfunc main() {\n\n\tfmt.Println()\n}\n```\n\nAfter.",
		"| a | b |\n| - | - |\n| 1 | 2 |\n\nAfter the table.",
		strings.Repeat("Paragraph. \n\n", 40),
	}
	for _, text := range texts {
		diff, err := DiffSource(text, text, true)
		if err != nil {
			t.Fatalf("DiffSource(%q): %v", firstLines(text), err)
		}
		for _, c := range diff.Changes {
			if c.Kind != Unchanged {
				t.Errorf("a text against itself reports %s at %d/%d: %q",
					c.Kind, c.SourceIndex, c.TargetIndex, firstLines(text))
				break
			}
		}
		if diff.All {
			t.Errorf("a text against itself reports the whole field as changed: %q", firstLines(text))
		}
	}
}

// TestAFencedBlockWithABlankLineIsOneParagraph is C20, and it is the reason the
// split is the renderer's AST and not a regular expression: a fenced block and
// a GFM table contain blank lines that are not paragraph breaks, and a split
// that mis-reads one reports a rewritten document where nothing was rewritten.
func TestAFencedBlockWithABlankLineIsOneParagraph(t *testing.T) {
	before := "Before.\n\n```go\nfunc main() {\n\n\tfmt.Println(\"hi\")\n}\n```\n\nAfter."
	after := before
	diff, err := DiffSource(before, after, true)
	if err != nil {
		t.Fatalf("DiffSource: %v", err)
	}
	for _, c := range diff.Changes {
		if c.Kind != Unchanged {
			t.Errorf("the same document with a blank line inside a fenced block reports %s: the code block was split", c.Kind)
			break
		}
	}

	// And the paragraph count proves the split happened where a translator
	// would draw the line: three blocks, not five lines.
	left, err := paragraphsOf(before, true)
	if err != nil {
		t.Fatalf("paragraphsOf: %v", err)
	}
	if len(left) != 3 {
		t.Errorf("the document split into %d paragraphs, want three: %q", len(left), left)
	}
}

// TestAReplacedBlockMarksBothSides covers the n↔m case: the honest answer is
// that the block was rewritten, and the view says so, rather than inventing a
// mapping between the second of three old paragraphs and the first of two new
// ones.
func TestAReplacedBlockMarksBothSides(t *testing.T) {
	before := "Anchor.\n\nOld one.\n\nOld two.\n\nOld three.\n\nAnchor."
	after := "Anchor.\n\nNew one.\n\nNew two.\n\nAnchor."
	diff, err := DiffSource(before, after, false)
	if err != nil {
		t.Fatalf("DiffSource: %v", err)
	}
	sources, targets := map[int]bool{}, map[int]bool{}
	for _, c := range diff.Changes {
		if c.Kind == Changed {
			sources[c.SourceIndex], targets[c.TargetIndex] = true, true
		}
	}
	// SourceIndex walks the source as it now stands (two new paragraphs) and
	// TargetIndex the source the translation was made from (three old ones).
	if len(sources) != 2 || len(targets) != 3 {
		t.Errorf("a replacement of three paragraphs by two marks %d current and %d old paragraphs, want 2 and 3",
			len(sources), len(targets))
	}
}

// TestAnAddedParagraphAndARemovedOne are the two one-sided answers: the source
// gained text nobody translated, and the source lost text somebody did.
func TestAnAddedParagraphAndARemovedOne(t *testing.T) {
	diff, err := DiffSource("One.\n\nTwo.", "One.\n\nTwo.\n\nThree.", false)
	if err != nil {
		t.Fatalf("DiffSource: %v", err)
	}
	added := 0
	for _, c := range diff.Changes {
		if c.Kind == Added {
			added++
			if c.TargetIndex != -1 {
				t.Errorf("an added paragraph names target %d, which has nothing to point at", c.TargetIndex)
			}
		}
	}
	if added != 1 {
		t.Errorf("the diff found %d added paragraphs, want one", added)
	}

	diff, err = DiffSource("One.\n\nTwo.\n\nThree.", "One.\n\nThree.", false)
	if err != nil {
		t.Fatalf("DiffSource: %v", err)
	}
	removed := 0
	for _, c := range diff.Changes {
		if c.Kind == Removed {
			removed++
		}
	}
	if removed != 1 {
		t.Errorf("the diff found %d removed paragraphs, want one", removed)
	}
}

// TestPlainNormalisationIsNotMarkdown is why a plain field never reaches
// goldmark: a title containing an asterisk is a title containing an asterisk.
func TestPlainNormalisationIsNotMarkdown(t *testing.T) {
	got := NormalisePlain("2 * 3 = 6  \r\n\r\n\r\n\r\nunder_score  \r\n")
	want := "2 * 3 = 6\n\nunder_score"
	if got != want {
		t.Errorf("NormalisePlain = %q, want %q", got, want)
	}
	// CRLF, a trailing space and a trailing newline are one source spelled
	// three ways, and they hash the same: that is the whole reason normalising
	// before hashing exists. A blank line between them is a different text, and
	// hashes differently, which is the other half of the same sentence.
	a, err := Hash("a\r\nb", false)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	b, _ := Hash("a  \nb \n", false)
	c, _ := Hash("a\nb", false)
	if a != b || a != c {
		t.Errorf("one plain source spelled three ways hashed %q, %q, %q", a, b, c)
	}
	if d, _ := Hash("a\n\nb", false); d == a {
		t.Error("a blank line hashes as no blank line")
	}
	// And an asterisk survived: it is still a title containing an asterisk.
	if strings.Contains(got, "<em>") || strings.Contains(got, "_under_score_") {
		t.Errorf("the plain normaliser interpreted markup: %q", got)
	}
}

// TestTheBoundRefusesAParagraphLevelClaim states the fallback rather than
// leaving it to be discovered by a page that costs thirty seconds.
func TestTheBoundRefusesAParagraphLevelClaim(t *testing.T) {
	small := strings.Repeat("Paragraph.\n\n", maxParagraphs+10)
	diff, err := DiffSource(small, small, false)
	if err != nil {
		t.Fatalf("DiffSource: %v", err)
	}
	if !diff.All {
		t.Error("a field past the bound made a paragraph-level claim")
	}
	if len(diff.Changes) != 0 {
		t.Errorf("the bound returned %d paragraph claims, want none", len(diff.Changes))
	}
}

func firstLines(s string) string {
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}
