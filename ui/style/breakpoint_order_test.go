package style_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/style"
)

func TestResponsiveRulesApplyFromNarrowToWideWithoutMutatingInputs(t *testing.T) {
	classes := []string{"lg:grid-cols-3", "sm:grid-cols-2", "md:grid-cols-4", "grid-cols-1"}
	before := slices.Clone(classes)
	sheet, err := style.Rules(classes...)
	if err != nil {
		t.Fatal(err)
	}
	text := sheet.CSS()
	previous := -1
	for _, rule := range []string{".grid-cols-1", "(min-width: 640px)", "(min-width: 768px)", "(min-width: 1024px)"} {
		at := strings.Index(text, rule)
		if at < previous || at < 0 {
			t.Fatalf("responsive cascade is out of order: %s", text)
		}
		previous = at
	}
	if !slices.Equal(classes, before) {
		t.Fatal("caller class order was mutated")
	}
}
