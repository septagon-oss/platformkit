package richtext

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

// TestEquivalentSpellingsSerializeTogether is the round trip decision 0069 asks
// for, past one example each: two sources that parse to the same document must
// store the same bytes, hash the same and render the same, and the stored bytes
// must keep what the source meant wherever the meaning is the characters
// themselves — the spaces inside a fence are the value, not decoration.
func TestEquivalentSpellingsSerializeTogether(t *testing.T) {
	for _, tc := range []struct {
		name  string
		left  string
		right string
	}{
		{"bullet markers", "+ first\n+ second", "- first\n- second"},
		{"star bullets", "* first\n* second", "- first\n- second"},
		{"setext heading", "Opening hours\n-------------", "## Opening hours"},
		{"indented code", "    make things\n\nafter", "```\nmake things\n```\n\nafter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			left, err := Normalise(tc.left)
			if err != nil {
				t.Fatalf("%q: %v", tc.left, err)
			}
			right, err := Normalise(tc.right)
			if err != nil {
				t.Fatalf("%q: %v", tc.right, err)
			}
			if left != right {
				t.Fatalf("equivalent sources stored differently: %q and %q", left, right)
			}
			leftHash, err := SourceHash(tc.left)
			if err != nil {
				t.Fatal(err)
			}
			rightHash, err := SourceHash(tc.right)
			if err != nil {
				t.Fatal(err)
			}
			if leftHash != rightHash {
				t.Fatalf("equivalent sources hash %s and %s", leftHash, rightHash)
			}
			if again, err := Normalise(left); err != nil || again != left {
				t.Fatalf("not a fixed point: %q then %q (%v)", left, again, err)
			}
		})
	}
}

// TestNormaliseKeepsWhatTheReaderSees is the invariant behind both: the stored
// value renders as the submitted one did, for each construct the subset allows.
func TestNormaliseKeepsWhatTheReaderSees(t *testing.T) {
	for _, source := range []string{
		"## Opening\n\nOne paragraph.\n\n- a\n- b\n\n> quoted\n\n```go\nfmt.Println(1) // trailing  \n```\n",
		"* plus\n* marks\n\n### H3\n\n| A | B |\n| --- | --- |\n| x | y |",
		"Text with ~~strike~~, `code  ` and a [link](https://example.test).\n\n1. one\n2. two",
		"A hard break at the end.  \nAnd the line after it.\n\n---\n\n#### H4",
		"~~~text\nliteral   \n\nstill literal\n~~~",
	} {
		t.Run(source[:24], func(t *testing.T) {
			before, err := Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			beforeHTML, err := Render(context.Background(), db.Tx[db.Tenant]{}, before, nil, Workspace)
			if err != nil {
				t.Fatal(err)
			}
			normal, err := Normalise(source)
			if err != nil {
				t.Fatalf("%q: %v", source, err)
			}
			after, err := Parse(normal)
			if err != nil {
				t.Fatalf("stored value does not parse: %q: %v", normal, err)
			}
			afterHTML, err := Render(context.Background(), db.Tx[db.Tenant]{}, after, nil, Workspace)
			if err != nil {
				t.Fatal(err)
			}
			if beforeHTML != afterHTML {
				t.Fatalf("normalisation changed the rendered document:\nstored %q\nbefore %q\nafter  %q", normal, beforeHTML, afterHTML)
			}
		})
	}
}
