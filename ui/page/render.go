package page

// render.go is how a page becomes an httpx.Page: a node rendered as a whole
// document or as a fragment, and the inline script a document may carry.
//
// These three came from kit/httpx, which had lifted them from modules/admin
// after a client's storefront copied them and the copy drifted — its inline
// script carried no nonce, so the content security policy dropped every page it
// rendered. The kernel keeps what is about the response: httpx.Page, Redirect,
// SeeOther and the HTML mount. Turning markup into bytes is presentation, and
// this package is where the shells already agree on it, so the kernel no longer
// imports the markup library at all.

import (
	"context"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// Render renders a node as a whole HTML document: the doctype, and then the
// node. Without the doctype a browser parses the page in quirks mode, which is
// a stylesheet that behaves differently for a reason nobody will find.
func Render(node g.Node, status int) (*httpx.Page, error) { return renderPage(node, status, true) }

// RenderFragment renders a node as itself, with no doctype: a response htmx
// swaps into a page that is already open.
//
// It is the same content type, because a fragment is HTML. What differs is that
// the browser is not being asked to make a document out of it, and a doctype in
// the middle of a page is what a browser does the strangest things with. The
// admin shell swaps whole pages and has no use for it; a storefront that
// replaces a cart badge does, which is why it is here rather than there — the
// copy that had to exist somewhere is this one.
func RenderFragment(node g.Node, status int) (*httpx.Page, error) {
	return renderPage(node, status, false)
}

func renderPage(node g.Node, status int, document bool) (*httpx.Page, error) {
	var b strings.Builder
	if document {
		b.WriteString("<!doctype html>")
	}
	if err := node.Render(&b); err != nil {
		return nil, err
	}
	return &httpx.Page{Status: status, ContentType: httpx.HTMLContentType, Body: []byte(b.String())}, nil
}

// InlineScript is an inline <script> with this request's content security
// policy nonce on it. A page that must run something before the first paint
// uses it; every other script is a file under the shell's assets and needs
// nothing.
//
// It exists because the alternative is remembering: the policy kit/httpx sets
// allows an inline script only with the nonce, so a tag written without one is
// dropped by the browser and reported in a console nobody is reading. This is
// the shape that cannot be written wrong; httpx.Nonce is the value it reads.
func InlineScript(ctx context.Context, js string) g.Node {
	return h.Script(g.Attr("nonce", httpx.Nonce(ctx)), g.Raw(js))
}
