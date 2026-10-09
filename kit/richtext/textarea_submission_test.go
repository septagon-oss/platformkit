package richtext

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

var blockTag = regexp.MustCompile(`</?[a-z0-9]+`)

// shapeOf is the sequence of element tags a document renders to, without text
// or attributes, so two documents compare by structure alone.
func shapeOf(t *testing.T, source string) string {
	t.Helper()
	d, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	html, err := Render(context.Background(), db.Tx[db.Tenant]{}, d, nil, Workspace)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(blockTag.FindAllString(html, -1), ">")
}

// A browser submits a textarea with CRLF line ends, and people leave spaces at
// the ends of lines. Such a body is accepted, stored once, and reads back with
// the structure and the words its author typed.
func TestATextareaSubmissionIsStoredAsItReads(t *testing.T) {
	for _, source := range []string{
		"## Opening hours\r\n\r\n- Monday \r\n- Tuesday\t\r\n  - morning \r\n\r\nSee [the site](https://example.com). \r\n",
		"> A quote \r\n> continued\r\n\r\n1. one \r\n2. two\r\n",
		"Intro \r\n\r\n```go\r\nfmt.Println(1)  \r\n```\r\n",
		"| a | b |\r\n|---|---|\r\n| 1 | 2 | \r\n",
		"Line with a break  \r\nnext line\r\n",
	} {
		stored, err := Normalise(source)
		if err != nil {
			t.Fatalf("Normalise(%q) refused: %v", source, err)
		}
		if strings.Contains(stored, "\r") || !strings.HasSuffix(stored, "\n") {
			t.Errorf("Normalise(%q) = %q, want LF-terminated lines", source, stored)
		}
		if again, err := Normalise(stored); err != nil || again != stored {
			t.Errorf("stored %q normalises to %q, %v", stored, again, err)
		}
		if before, after := shapeOf(t, source), shapeOf(t, stored); before != after {
			t.Errorf("storing %q changed its structure: %s became %s", source, before, after)
		}
		src, _ := Parse(source)
		dst, _ := Parse(stored)
		if PlainText(src) != PlainText(dst) {
			t.Errorf("storing %q changed its words: %q became %q", source, PlainText(src), PlainText(dst))
		}
	}
}
