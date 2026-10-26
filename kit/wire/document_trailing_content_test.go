package wire_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// TestADocumentWithTrailingContentRefusesAsB3 keeps a decoder's patience from widening what
// the gate accepts: a document followed by a second JSON value is not one contract, and a
// side that cannot be read as a whole is not trusted. A writer's own trailing whitespace is
// not content.
func TestADocumentWithTrailingContentRefusesAsB3(t *testing.T) {
	for _, tc := range []struct {
		name, suffix, says string
		readable           bool
	}{
		{"a second document", `{"openapi":"3.1.0","paths":{}}`, "trailing content after the document", false},
		{"a stray array", `[]`, "trailing content after the document", false},
		{"bytes that never close", `{`, "trailing content", false},
		{"the newline a writer appends", "\n", "", true},
		{"the indentation a writer appends", " \t\r\n", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, kind := range []string{"openapi", "asyncapi"} {
				body := append(bytes.Clone(baseline(kind)), tc.suffix...)
				got := wire.Compare(body, body)
				if tc.readable {
					if len(got) != 0 {
						t.Fatalf("%s with trailing whitespace broke: %#v", kind, got)
					}
					continue
				}
				// Both sides are the same bytes, so the two refusals differ only in the
				// side they name, and results sort by message: current first.
				if len(got) != 2 || got[0].Member != "current" || got[1].Member != "golden" {
					t.Fatalf("%s: want both sides refused, got %#v", kind, got)
				}
				for _, problem := range got {
					if problem.Rule != "B3" || problem.Path != "$" ||
						!strings.Contains(problem.Message, "document is invalid") || (tc.says != "" && !strings.Contains(problem.Message, tc.says)) {
						t.Fatalf("%s: %#v does not name %q", kind, problem, tc.says)
					}
				}
			}
		})
	}
}
