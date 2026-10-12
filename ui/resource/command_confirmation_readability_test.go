package resource_test

import (
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Confirmation copy joins form help and shell copy on reference detail pages.
// Use the same small text and bounded measure as the existing form/frame
// sentences: an unstyled paragraph adds a third body size and spans 142ch.
func TestCommandConfirmationKeepsTheFormTextScaleAndBoundedMeasure(t *testing.T) {
	for _, warning := range []string{
		"It stops being served. You can publish it again later.",
		"They will not be able to sign in. An administrator can reactivate them.",
	} {
		r := withCommands(resource.Command{Verb: "archive", Label: "Archive",
			Present: entity.CommandHints{Confirmation: &entity.CommandConfirmation{
				Title: "Archive this record?", Body: warning, ConfirmLabel: "Archive"}}})
		doc, err := html.Parse(strings.NewReader(rowPage(t, r)))
		if err != nil {
			t.Fatal(err)
		}
		var paragraph *html.Node
		for n := range doc.Descendants() {
			if n.Type == html.TextNode && n.Data == warning {
				paragraph = n.Parent
				break
			}
		}
		if paragraph == nil {
			t.Fatalf("the declared confirmation %q was not rendered", warning)
		}
		var classes []string
		for _, attr := range paragraph.Attr {
			if attr.Key == "class" {
				classes = strings.Fields(attr.Val)
			}
		}
		for _, want := range []string{"text-sm", "max-w-sm"} {
			if !slices.Contains(classes, want) {
				t.Errorf("confirmation %q lacks %s; classes %v leave its text scale or reading measure uncontrolled", warning, want, classes)
			}
		}
	}
}
