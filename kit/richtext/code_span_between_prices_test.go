package richtext

import (
	"strings"
	"testing"
)

// The math matcher reads the masked line, and masking blanks a code span's
// content but not its backticks. The fragile consequence: a code span sitting
// between two prices leaves a `$…$` span whose masked content must carry no TeX
// mark, or the prose around literal code would be refused as a formula the
// author never wrote.
func TestAPriceOnEachSideOfACodeSpanStaysProse(t *testing.T) {
	for _, source := range []string{
		"pay $5, run `x^2`, get $8\n",
		"from $5 via `a_b{c}` up to $8\n",
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
