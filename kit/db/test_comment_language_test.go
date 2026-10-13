package db_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"testing"
)

// Migration test comments explain the SQL shape and the state it protects. They
// must stand on their own when the discussion that prompted a test is unavailable.
func TestMigrationTestCommentsExplainBehaviorWithoutReviewHistory(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no migration tests found")
	}
	history := regexp.MustCompile(`(?i)\b(?:review's (?:case|fourth finding)|same review|previous round|round's call|(?:first|second) finding)\b`)
	positions := token.NewFileSet()
	for _, name := range files {
		file, err := parser.ParseFile(positions, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if history.MatchString(comment.Text) {
					t.Errorf("%s: explain the protected behavior without discussion history: %s", positions.Position(comment.Pos()), comment.Text)
				}
			}
		}
	}
}
