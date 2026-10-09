package richtext

import (
	"errors"
	"testing"
)

// TestNormaliseRefusesTextWhoseStoredFormReadsDifferently pins each half of the
// guard on a value that reaches it. Trimming a line is safe only while the next
// parse still reads the same document, and two ordinary characters show the way
// it can stop reading one: the tab after an incomplete tag, which leaves an HTML
// block when it is trimmed, and the space after a line-final backslash, which is
// what keeps a backslash from being a hard break. Both are refused, because a
// stored value the next write refuses cannot be repaired by the author who finds
// it, and the refusal names the construct, the line and the message key.
func TestNormaliseRefusesTextWhoseStoredFormReadsDifferently(t *testing.T) {
	for _, tc := range []struct{ source, construct string }{
		{"<p\t", "raw HTML"},
		{"text \\ \nmore", "unstorable text"},
	} {
		_, err := Normalise(tc.source)
		var refused *Refused
		if !errors.As(err, &refused) || len(refused.Issues) == 0 {
			t.Fatalf("Normalise(%q) = %v, want a refusal naming a construct", tc.source, err)
		}
		issue := refused.Issues[0]
		if issue.Construct != tc.construct || issue.Line < 1 || issue.Remedy == "" || issue.Key() == "" {
			t.Errorf("Normalise(%q) refused with %+v, want %s on a line with a remedy and a message key", tc.source, issue, tc.construct)
		}
	}
}
