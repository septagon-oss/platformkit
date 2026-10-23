package resource_test

import (
	"regexp"
	"testing"

	"github.com/septagon-oss/platformkit/ui/document"
)

var controlIDAttr = regexp.MustCompile(`id="([^"]+)"`)

func TestCommandControlIDsDifferBetweenRecords(t *testing.T) {
	seen := map[string]string{}
	for _, row := range []string{"one", "two"} {
		view := commandDetail(sharedArgumentCommands(), row, commandLocales())
		out, err := document.Render(document.Document(document.Chrome{}, document.Request{}, view, document.Bare(view.Body)))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range controlIDAttr.FindAllStringSubmatch(string(out), -1) {
			if prev, ok := seen[m[1]]; ok && prev != row {
				t.Errorf("id %q is rendered for records %s and %s", m[1], prev, row)
			}
			seen[m[1]] = row
		}
	}
}
