package resource_test

import (
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
	"github.com/septagon-oss/platformkit/ui/style"
)

// floorCommands are the row and collection commands a busy entity declares: three
// on a record, one of them with confirmation copy, and one over the whole list.
var floorCommands = []resource.Command{
	assignCommand,
	{Verb: "resolve", Label: "Resolve", Base: "/app/note/notes"},
	{Verb: "archive", Label: "Archive", Base: "/app/note/notes",
		Present: entity.CommandHints{Confirmation: &entity.CommandConfirmation{
			Title: "Archive this note?", Body: "It leaves the list. You can restore it later.", ConfirmLabel: "Archive"}}},
	sweepCommand,
}

// TestAGeneratedScreenFillsOneActionAndBoundsItsBodySizes holds a generated record
// and list to two of the design floor's rules whatever commands the entity
// declares: at most one control carries the brand fill (Edit on a record, New on
// a list), and the paragraphs use at most two text sizes. A paragraph naming no
// size inherits the page's, and counts as that one size.
func TestAGeneratedScreenFillsOneActionAndBoundsItsBodySizes(t *testing.T) {
	r := withCommands(floorCommands...)
	for name, page := range map[string]string{"record": rowPage(t, r), "list": listPage(t, r)} {
		doc, err := html.Parse(strings.NewReader(page))
		if err != nil {
			t.Fatal(err)
		}
		filled, sizes := 0, map[string]bool{}
		for n := range doc.Descendants() {
			if n.Type != html.ElementNode {
				continue
			}
			size := "inherited"
			for _, class := range classes(n) {
				if (n.Data == "a" || n.Data == "button") && class == fill {
					filled++
				}
				if textSizes[class] {
					size = class
				}
			}
			if n.Data == "p" {
				sizes[size] = true
			}
		}
		if filled > 1 {
			t.Errorf("the generated %s fills %d actions with the brand colour; the design floor allows one", name, filled)
		}
		if len(sizes) > 2 {
			t.Errorf("the generated %s's paragraphs use %d text sizes %v; the design floor allows two", name, len(sizes), sizes)
		}
	}
}

// TestAnEmptyGeneratedListDrawsNoParagraphSizeAListWithRowsDoesNot holds the list
// with no rows to the sizes the same list draws with rows. The frame's chrome
// takes one of the floor's two body sizes, so a body measured without the frame
// can stay under two and still add the composed page's third; a subset of the
// sizes a list with rows uses cannot.
func TestAnEmptyGeneratedListDrawsNoParagraphSizeAListWithRowsDoesNot(t *testing.T) {
	r := withCommands(floorCommands...)
	rows := paragraphSizes(t, listPage(t, r))
	empty := render(t, resource.List(r, opts, nil, 0, 1, "", false).Body)
	said := false
	for size, texts := range paragraphSizes(t, empty) {
		said = said || slices.Contains(texts, "No notes yet.")
		if _, ok := rows[size]; !ok {
			t.Errorf("the empty generated list sets %q in %s, which the list with rows never uses %v", texts, size, rows)
		}
	}
	if !said {
		t.Errorf("no paragraph of the list with no rows says it is empty:\n%s", around(empty, "<table"))
	}
}

// paragraphSizes groups a page's paragraphs by the text-size class they carry,
// with "inherited" for a paragraph that names none, and keeps each one's text.
func paragraphSizes(t *testing.T, page string) map[string][]string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	sizes := map[string][]string{}
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode || n.Data != "p" {
			continue
		}
		size := "inherited"
		for _, class := range classes(n) {
			if textSizes[class] {
				size = class
			}
		}
		var text strings.Builder
		for d := range n.Descendants() {
			if d.Type == html.TextNode {
				text.WriteString(d.Data)
			}
		}
		sizes[size] = append(sizes[size], strings.TrimSpace(text.String()))
	}
	return sizes
}

func classes(n *html.Node) []string {
	for _, attr := range n.Attr {
		if attr.Key == "class" {
			return strings.Fields(attr.Val)
		}
	}
	return nil
}

// fill is the brand-filled background a primary button paints, and textSizes
// the class of every size on the type scale, both as the style package compiles
// them, so a renamed utility changes these with it.
var (
	fill      = style.New().Bg(style.SurfaceBrand).Compile()
	textSizes = func() map[string]bool {
		out := map[string]bool{}
		for _, s := range style.AllFontSizes() {
			out[style.New().FontSize(s).Compile()] = true
		}
		return out
	}()
)
