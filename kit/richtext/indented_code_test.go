package richtext

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

// storedRound renders the source, stores it, renders what was stored, and stores
// that again. Every case below is judged on the pair: the reader must be shown
// the same document, and the second store must change nothing.
func storedRound(t *testing.T, source string) (before, after, stored string) {
	t.Helper()
	show := func(s string) string {
		d, err := Parse(s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		html, err := Render(context.Background(), db.Tx[db.Tenant]{}, d, nil, Workspace)
		if err != nil {
			t.Fatalf("render %q: %v", s, err)
		}
		return html
	}
	before = show(source)
	var err error
	stored, err = Normalise(source)
	if err != nil {
		t.Fatalf("store %q: %v", source, err)
	}
	after = show(stored)
	again, err := Normalise(stored)
	if err != nil || again != stored {
		t.Fatalf("stored value is not a fixed point: %q then %q: %v", stored, again, err)
	}
	return before, after, stored
}

// Indented code that lives inside another container is indented relative to
// that container. A fence written at column zero would close the container, so
// those lines are kept exactly as they were submitted.
func TestIndentedCodeInsideAContainerStaysWhereTheContainerPutIt(t *testing.T) {
	for _, source := range []string{
		"- item\n\n      code inside the item",
		"1. step\n\n       code under the step\n\n2. next",
		"> quoted\n\n>     code inside the quotation",
	} {
		t.Run(source, func(t *testing.T) {
			before, after, stored := storedRound(t, source)
			if before != after {
				t.Errorf("storing changed what the reader sees:\n stored %q\n before %q\n after  %q", stored, before, after)
			}
			if !strings.Contains(after, "<code>") {
				t.Errorf("the indented code stopped being code: %q", after)
			}
		})
	}
}

// At the top level an indented code block does become a fence, and the fence is
// the width the block's own content asks for, so a line of backticks stays data.
func TestIndentedCodeAtTheTopLevelBecomesAFenceWideEnoughToHoldIt(t *testing.T) {
	for _, tc := range []struct{ source, shows, stores string }{
		{"\tcode\n\nafter", "<pre><code>code\n</code></pre>", "```\ncode\n```\n\nafter\n"},
		{"    a\n  \n    b", "<pre><code>a\n\nb\n</code></pre>", "```\na\n\nb\n```\n"},
		{"    ````x\n    y", "<pre><code>````x\ny\n</code></pre>", "`````\n````x\ny\n`````\n"},
		{"    a\n\n\n    b", "<pre><code>a\n\n\nb\n</code></pre>", "```\na\n\n\nb\n```\n"},
		// A Setext heading is stored as one line where the source wrote two, so
		// the code below it no longer sits on the line it was submitted on.
		{"Opening\n--------\n\n    code\n\n- a", "<pre><code>code\n</code></pre>", "## Opening\n\n```\ncode\n```\n\n- a\n"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			before, after, stored := storedRound(t, tc.source)
			if before != after {
				t.Errorf("storing changed what the reader sees:\n stored %q\n before %q\n after  %q", stored, before, after)
			}
			if !strings.Contains(after, tc.shows) {
				t.Errorf("stored %q renders %q, which does not show %q", stored, after, tc.shows)
			}
			if stored != tc.stores {
				t.Errorf("stored %q, want %q", stored, tc.stores)
			}
		})
	}
}

// Two lists written with different bullet markers are two lists. Storing one
// spelling of the marker would fuse them, so a list with a list beside it keeps
// the marker its author typed.
func TestListsThatDifferOnlyInMarkerStayTwoLists(t *testing.T) {
	for _, tc := range []struct{ source, shows string }{
		{"* a\n- b", "</ul>\n<ul>"},
		{"* a\n\n- b", "</ul>\n<ul>"},
		{"- a\n  * b\n  + c", "</ul>\n<ul>"},
		{"+ a\n+ b", "<ul>\n<li>a</li>\n<li>b</li>\n</ul>"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			before, after, stored := storedRound(t, tc.source)
			if before != after {
				t.Errorf("storing changed what the reader sees:\n stored %q\n before %q\n after  %q", stored, before, after)
			}
			if !strings.Contains(after, tc.shows) {
				t.Errorf("stored %q renders %q, which does not show %q", stored, after, tc.shows)
			}
		})
	}
}
