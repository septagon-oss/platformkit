package richtext

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

// A line indented four spaces after a blank line is indented code only where
// the parser says it is. Inside a list item it is a continuation paragraph or a
// nested list, and an indented code block's own lines may hold a fence. Storing
// any of them must keep what the reader sees, and a second pass must change
// nothing.
func TestNormaliseKeepsIndentedListContentAndCode(t *testing.T) {
	cases := []struct {
		name   string
		source string
		shows  string // what the source itself renders, before any normalisation
	}{
		{"a continuation paragraph of a numbered step", "1. Open the door\n\n    Then walk in.\n\n2. Sit down", "<li>\n<p>Open the door</p>\n<p>Then walk in.</p>\n</li>"},
		{"a nested list after a blank line", "- fruit\n\n    - apple\n    - pear", "<p>fruit</p>\n<ul>\n<li>apple</li>"},
		{"indented code that holds a fence", "Intro:\n\n    ```go\n    fmt.Println(1)\n    ```", "<pre><code>```go\nfmt.Println(1)\n```\n</code></pre>"},
		{"indented code with a blank run inside", "text\n\n    code\n\n\n    more", "<pre><code>code\n\n\nmore\n</code></pre>"},
	}
	render := func(t *testing.T, source string) string {
		t.Helper()
		d, err := Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		html, err := Render(context.Background(), db.Tx[db.Tenant]{}, d, nil, Workspace)
		if err != nil {
			t.Fatal(err)
		}
		return html
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := render(t, c.source)
			if !strings.Contains(before, c.shows) {
				t.Fatalf("the source does not render the structure under test: %q", before)
			}
			stored, err := Normalise(c.source)
			if err != nil {
				t.Fatal(err)
			}
			if after := render(t, stored); after != before {
				t.Errorf("storing changed what the reader sees:\n stored %q\n before %q\n after  %q", stored, before, after)
			}
			again, err := Normalise(stored)
			if err != nil {
				t.Fatal(err)
			}
			if again != stored {
				t.Errorf("Normalise is not idempotent: %q then %q", stored, again)
			}
		})
	}
}
