package richtext

import (
	"errors"
	"testing"
)

// An angle-bracket autolink is a link like any other: its destination passes
// the same scheme rule as [text](url), and the refusal names the autolink's own
// line rather than the document's first.
func TestAnAutolinkOutsideTheAllowedSchemesIsRefused(t *testing.T) {
	for _, destination := range []string{"javascript:alert(1)", "data:text/html,x", "ftp://example.test/x", "https://user:pass@example.test"} {
		_, err := Normalise("Opening hours\n\n<" + destination + ">\n")
		var refused *Refused
		if !errors.As(err, &refused) || len(refused.Issues) != 1 {
			t.Errorf("<%s>: %v, want one refusal", destination, err)
			continue
		}
		if issue := refused.Issues[0]; issue.Key() != "link-destination" || issue.Line != 3 {
			t.Errorf("<%s>: refused as %q on line %d, want link-destination on line 3", destination, issue.Key(), issue.Line)
		}
	}
	for _, source := range []string{"<mailto:info@example.test>\n", "<HTTPS://example.test/page>\n"} {
		normal, err := Normalise(source)
		if err != nil || normal != source {
			t.Errorf("%q: stored %q, %v; want it stored as written", source, normal, err)
		}
	}
}
