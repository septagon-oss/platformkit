package resource_test

import (
	"regexp"
	"testing"

	"github.com/septagon-oss/platformkit/ui/document"
)

var (
	controlRefAttr = regexp.MustCompile(`(?:for|aria-describedby|aria-errormessage)="([^"]+)"`)
	controlIDAttrs = regexp.MustCompile(`\bid="([^"]+)"`)
)

func TestCommandControlReferencesResolveToElementsOnThePage(t *testing.T) {
	view := commandDetail(sharedArgumentCommands(), "one", commandLocales())
	out, err := document.Render(document.Document(document.Chrome{}, document.Request{}, view, document.Bare(view.Body)))
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, m := range controlIDAttrs.FindAllStringSubmatch(string(out), -1) {
		ids[m[1]] = true
	}
	for _, m := range controlRefAttr.FindAllStringSubmatch(string(out), -1) {
		for _, ref := range regexp.MustCompile(`\S+`).FindAllString(m[1], -1) {
			if !ids[ref] {
				t.Errorf("reference %q names no element on the page", ref)
			}
		}
	}
}
