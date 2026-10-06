package appname_test

import (
	"path/filepath"
	"testing"
)

// TestTheCensusSeesANameSpelledOutOfLiteralsAndConstants widens the census's own
// planted claim to the shapes a caller would actually write. The census states
// that a shared name formed anywhere but in this package is a bug it finds; each
// of its eight patterns matches one spelling of one expression, so the same name
// spelled a different way passes the gate. testdata/planted_literals plants three,
// the forms the tree already reaches for:
//
//   - a cookie name as one whole literal — the cookie rule needs the string to
//     end where the host-only prefix ends, and a full cookie name never ends
//     there;
//   - an event address built from the namespace written out in the line itself
//     rather than from the transport's prefix constant — the subject rule looks
//     for the constant;
//   - a durable whose join is spelled through strings.Join rather than the one
//     expression kit/events writes today — the durable rule names those operands.
//
// The first of these already exists in the tree outside this package:
// modules/auth/internal/http_test.go:644 asserts the whole cookie name as a
// literal, and the census is green over it, so the blind spot is not hypothetical.
func TestTheCensusSeesANameSpelledOutOfLiteralsAndConstants(t *testing.T) {
	got := scan(t, filepath.Join("testdata", "planted_literals"))
	seen := map[string]int{}
	for _, f := range got {
		seen[f.rule]++
		t.Logf("reported: %s", f)
	}
	for _, name := range []string{
		"session and first-party cookie names",
		"event subjects and filters",
		"durable consumer names",
	} {
		if seen[name] == 0 {
			t.Errorf("the census does not see %s spelled as a whole literal, an inline prefix or a different join, so that spelling of a shared name passes the gate", name)
		}
	}
	if len(got) != 3 {
		t.Errorf("the planted file forms 3 shared names and the census reported %d: %v", len(got), got)
	}
}
