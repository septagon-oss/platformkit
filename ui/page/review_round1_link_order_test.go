package page_test

// Review round 1 of T-0108: a second shell stylesheet now depends on the order
// the shell links it. gallery.css is `@layer components` since the cascade
// change, and a layer's rank comes from the order the layers are *first
// declared*, not from the order statement that names them all — measured in the
// review: with app.css first the components rule wins (rgb(9,9,9)), with
// gallery.css first the order statement in app.css re-ranks the layers behind
// it and the base rule wins (rgb(8,8,8)). The shell keeps that promise today by
// writing its own stylesheet before anything the view adds
// (ui/document/document.go:224). Nothing else reads the two link tags against
// each other, so this case pins it.

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
)

func TestReviewTheShellLinksItsOwnSheetBeforeAnythingTheViewAdds(t *testing.T) {
	t.Parallel()
	c := chrome()
	r := page.Request{SignedIn: true, Principal: tenancy.Principal{UserID: uuid.MustParse("bf81ba02-7ae1-468e-a908-842736ba7246")}}
	view := page.View{Title: "Gallery", Head: []g.Node{
		h.Link(h.Rel("stylesheet"), h.Href(c.Assets+"/gallery.css?v=0123456789abcdef")),
	}}
	out := render(t, page.Document(c, r, view, h.Main()))
	app := strings.Index(out, `href="`+c.Assets+`/app.css`)
	gallery := strings.Index(out, `href="`+c.Assets+`/gallery.css`)
	if app < 0 || gallery < 0 {
		t.Fatalf("the rendered head does not carry both sheets: app.css at %d, gallery.css at %d", app, gallery)
	}
	if app > gallery {
		t.Errorf("the view's stylesheet is linked before app.css (app.css at %d, gallery.css at %d), so app.css is no longer the first declaration of the layer set and the @layer order it states stops being the sheet's order", app, gallery)
	}
}
