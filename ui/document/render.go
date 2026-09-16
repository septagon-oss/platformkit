package document

// render.go is how a document becomes bytes: a node rendered as a whole
// document or as a fragment, and the inline script a document may carry. The
// response those bytes travel in — status, content type, headers — is
// kit/httpx's Page, and ui/page wraps them into one.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// Render renders a node as a whole HTML document: the doctype, and then the
// node. Without the doctype a browser parses the page in quirks mode, which is
// a stylesheet that behaves differently for a reason nobody will find.
func Render(node g.Node) ([]byte, error) { return render(node, true) }

// RenderFragment renders a node as itself, with no doctype: a response htmx
// swaps into a page that is already open.
//
// It is the same content type, because a fragment is HTML. What differs is that
// the browser is not being asked to make a document out of it, and a doctype in
// the middle of a page is what a browser does the strangest things with.
func RenderFragment(node g.Node) ([]byte, error) { return render(node, false) }

func render(node g.Node, document bool) ([]byte, error) {
	var b strings.Builder
	if document {
		b.WriteString("<!doctype html>")
	}
	if err := node.Render(&b); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// InlineScript is an inline <script> carrying the content security policy
// nonce of the response it belongs to. A page that must run something before
// the first paint uses it; every other script is a file under the shell's
// assets and needs nothing.
//
// The nonce is a parameter because it is the response's, minted by kit/httpx
// per request: ui/page.InlineScript reads it off the context and calls this.
// A tag written without one is dropped by the browser and reported in a
// console nobody is reading, which is why the shape takes the nonce rather than
// leaving it to be remembered.
func InlineScript(nonce, js string) g.Node {
	return h.Script(g.Attr("nonce", nonce), g.Raw(js))
}
