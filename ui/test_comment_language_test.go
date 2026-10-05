package ui_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"testing"
)

// UI test comments explain the protected behavior without requiring the discussion
// that introduced the test. Domain terms such as preview and round trip are allowed.
func TestUICommentsExplainBehaviorWithoutDiscussionHistory(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no UI tests found")
	}
	history := regexp.MustCompile(`(?i)\b(?:round's pin|belongs? to the delivery rather than to the review|is the finding)\b`)
	positions := token.NewFileSet()
	for _, name := range files {
		file, err := parser.ParseFile(positions, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		for _, group := range file.Comments {
			if history.MatchString(group.Text()) {
				t.Errorf("%s: explain behavior without discussion history: %s", positions.Position(group.Pos()), group.Text())
			}
		}
	}
}
