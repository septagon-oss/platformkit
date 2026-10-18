package ui_test

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
)

// focusReconciliation is the delayed check prosemirror-view schedules after the editor
// takes focus: the one 20 ms timer whose body compares the observer's last reading
// with the page's selection range. The bundle is minified, so the match names only
// property names, which minification keeps.
var focusReconciliation = regexp.MustCompile(`setTimeout\(\(\)=>\{([^{}]*\.domSelectionRange\(\)[^{}]*)\},20\)`)

// TestTheServedEditorReadsThePageBeforeItRestoresASelection reads the editor bundle a
// shell serves and checks that the focus reconciliation flushes the DOM observer
// before it compares selections. Without that read the timer writes the editor's
// stale caret over a range the keyboard made a moment earlier, and the words the
// author selected stop being selected (tools/richtext-bundle/build.mjs patches it in).
func TestTheServedEditorReadsThePageBeforeItRestoresASelection(t *testing.T) {
	bundle, err := fs.ReadFile(ui.Assets(ui.Compose(design.Default())), "richtext/editor.js")
	if err != nil {
		t.Fatalf("the shell serves no richtext/editor.js: %v", err)
	}
	found := focusReconciliation.FindAllSubmatch(bundle, -1)
	if len(found) != 1 {
		t.Fatalf("want exactly one focus reconciliation timer in the served bundle, found %d", len(found))
	}
	body := string(found[0][1])
	flush := strings.Index(body, ".domObserver.flush()")
	compare := strings.Index(body, ".domSelectionRange()")
	if flush < 0 || flush > compare || !strings.Contains(body[:compare], ".domObserver.forceFlush()") {
		t.Fatalf("the focus reconciliation compares and restores before it reads the page:\n%s", body)
	}
}
