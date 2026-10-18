package richtext

import "testing"

func TestEquivalentBulletMarkersHaveOneStoredSpelling(t *testing.T) {
	plus, err := Normalise("+ first\n+ second")
	if err != nil {
		t.Fatal(err)
	}
	minus, err := Normalise("- first\n- second")
	if err != nil {
		t.Fatal(err)
	}
	if plus != minus {
		t.Fatalf("equivalent lists have different stored Markdown: plus %q, minus %q", plus, minus)
	}
}
