package components

// The form's sentence ships on generated record pages, so every class it
// emits must be styled by the sheet ui.Compose serves from ShellClassLists;
// the gallery closure accepts a class only GalleryClassLists declares, and
// the served sentence would then paint unstyled at 16px across the page.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/style"
)

func TestTheFormSentencesClassesAreStyledByTheServedSheet(t *testing.T) {
	t.Parallel()
	sheet, err := style.For(ShellClassLists()...)
	if err != nil {
		t.Fatal(err)
	}
	html := renderNodeToString(t, FormSentence(FormSentenceProps{Text: "It stops being served."}))
	_, attrs, found := strings.Cut(html, `class="`)
	classes, _, _ := strings.Cut(attrs, `"`)
	if !found || classes == "" {
		t.Fatalf("the sentence renders no class of its own: %s", html)
	}
	for class := range strings.FieldsSeq(classes) {
		if !strings.Contains(sheet.CSS(), "."+class+" {") {
			t.Errorf("the shell's served sheet styles no %q: the sentence paints unbounded on a generated page", class)
		}
	}
}
