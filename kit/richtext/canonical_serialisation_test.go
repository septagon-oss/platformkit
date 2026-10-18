package richtext

import "testing"

func TestEquivalentHeadingsHaveOneStoredSpelling(t *testing.T) {
	setext, err := Normalise("Opening hours\n-------------")
	if err != nil {
		t.Fatal(err)
	}
	atx, err := Normalise("## Opening hours")
	if err != nil {
		t.Fatal(err)
	}
	if setext != atx {
		t.Fatalf("equivalent H2 headings have different stored Markdown: setext %q, ATX %q", setext, atx)
	}
}
