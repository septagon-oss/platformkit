package richtext

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

// The stored form of a link carries the author's own words about the destination,
// so the page has to show them: a renderer that writes the address and drops the
// title leaves the saved document saying more than the page ever will, and no
// further write can get the words back. The escaped quotes are the spelling that
// survives a save, and they mean the same here as they mean in the text of a
// paragraph — the title is shown with the author's quote in it and no backslash.
func TestALinksOwnTitleReachesThePage(t *testing.T) {
	stored, err := Normalise(`[Website](https://example.com "A \"quoted\" title")`)
	if err != nil {
		t.Fatalf("a link with a quoted title was refused at the write: %v", err)
	}
	doc, err := Parse(stored)
	if err != nil {
		t.Fatal(err)
	}
	page, err := Render(context.Background(), db.Tx[db.Tenant]{}, doc, nil, Workspace)
	if err != nil {
		t.Fatalf("a link with a quoted title did not render: %v", err)
	}
	want := `<p><a href="https://example.com" title="A &#34;quoted&#34; title" rel="noopener noreferrer nofollow ugc">Website</a></p>` + "\n"
	if page != want {
		t.Errorf("link with a title rendered\n%s\nwant\n%s", page, want)
	}
}

// A title is free text an author wrote, and free text is what the sanitizer exists
// for. The characters that would end the attribute and open another one stay inside
// the one attribute they arrived in: a title reaches the page as the words of the
// link and never as an event handler, an element or a second address.
func TestALinkTitleCarriesNoMarkup(t *testing.T) {
	stored, err := Normalise(`[Website](https://example.com "a \" onload=\"b")`)
	if err != nil {
		t.Fatalf("the write refused a title the stored form holds: %v", err)
	}
	page, err := RenderLegacy(stored)
	if err != nil {
		t.Fatalf("a hostile title did not render: %v", err)
	}
	want := `<p><a href="https://example.com" title="a &#34; onload=&#34;b" rel="noopener noreferrer nofollow ugc">Website</a></p>` + "\n"
	if page != want {
		t.Errorf("hostile title reached the page as\n%s\nwant\n%s", page, want)
	}
}
