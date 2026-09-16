package page

// render.go is how a document becomes an httpx.Page: ui/document renders the
// bytes, and this file puts them in the response shape the kernel serves and
// reads the one request value the pure renderer cannot — the nonce.
//
// These three came from kit/httpx, which had lifted them from modules/admin
// after a client's storefront copied them and the copy drifted — its inline
// script carried no nonce, so the content security policy dropped every page it
// rendered. The kernel keeps what is about the response: httpx.Page, Redirect,
// SeeOther and the HTML mount. Turning markup into bytes is presentation.

import (
	"context"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/document"
)

// Render renders a node as a whole HTML document, doctype first, as the page
// kit/httpx serves with status.
func Render(node g.Node, status int) (*httpx.Page, error) {
	body, err := document.Render(node)
	return respond(body, status, err)
}

// RenderFragment renders a node as itself, with no doctype: a response htmx
// swaps into a page that is already open. The admin shell swaps whole pages
// and has no use for it; a storefront that replaces a cart badge does.
func RenderFragment(node g.Node, status int) (*httpx.Page, error) {
	body, err := document.RenderFragment(node)
	return respond(body, status, err)
}

func respond(body []byte, status int, err error) (*httpx.Page, error) {
	if err != nil {
		return nil, err
	}
	return &httpx.Page{Status: status, ContentType: httpx.HTMLContentType, Body: body}, nil
}

// InlineScript is an inline <script> with this request's content security
// policy nonce on it. A page that must run something before the first paint
// uses it; every other script is a file under the shell's assets and needs
// nothing. httpx.Nonce is the value it reads; document.InlineScript is the shape.
func InlineScript(ctx context.Context, js string) g.Node {
	return document.InlineScript(httpx.Nonce(ctx), js)
}
