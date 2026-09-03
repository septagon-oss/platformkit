package httpx

// html.go is how this application serves HTML: one response shape, the two ways
// to render a node into it, and one function that mounts a page as an ordinary
// operation.
//
// It is in the kernel because it was in two places. modules/admin wrote it for
// the generated screens, a client's storefront copied it for its own, and the
// copy drifted where it mattered most: the storefront's inline script carried
// no nonce, so every page it rendered was one the content security policy would
// have dropped. A page is not a second kind of route — it is an operation whose
// body happens to be bytes — and this is the whole of what makes that true.

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// Page is an HTML response. huma writes a []byte body verbatim, so this is the
// whole of "serve HTML" — no second router, no second middleware chain, and the
// same recording, the same authorization declaration and the same transaction
// as every JSON route in the application.
type Page struct {
	Status      int
	ContentType string `header:"Content-Type"`
	Location    string `header:"Location"`
	HXRedirect  string `header:"HX-Redirect"`
	Body        []byte
}

// HTMLContentType is what every Page carries. It is exported because a handler
// that builds a Page itself — one that serves bytes it did not render from a
// node — should not spell it a second way.
const HTMLContentType = "text/html; charset=utf-8"

// Document renders a node as a whole HTML document: the doctype, and then the
// node. Without the doctype a browser parses the page in quirks mode, which is
// a stylesheet that behaves differently for a reason nobody will find.
func Document(node g.Node, status int) (*Page, error) { return render(node, status, true) }

// Fragment renders a node as itself, with no doctype: a response htmx swaps
// into a page that is already open.
//
// It is the same content type, because a fragment is HTML. What differs is that
// the browser is not being asked to make a document out of it, and a doctype in
// the middle of a page is what a browser does the strangest things with. The
// admin shell swaps whole pages and has no use for it; a storefront that
// replaces a cart badge does, which is why it is here rather than there — the
// copy that had to exist somewhere is this one.
func Fragment(node g.Node, status int) (*Page, error) { return render(node, status, false) }

func render(node g.Node, status int, document bool) (*Page, error) {
	var b strings.Builder
	if document {
		b.WriteString("<!doctype html>")
	}
	if err := node.Render(&b); err != nil {
		return nil, err
	}
	return &Page{Status: status, ContentType: HTMLContentType, Body: []byte(b.String())}, nil
}

// Redirect sends the caller somewhere else after a write. htmx does not follow
// a 303 the way a browser does — it would swap the target page into a fragment
// — so a request it made is answered with the header it understands instead.
func Redirect(ctx context.Context, to string) *Page {
	if r, ok := RequestFrom(ctx); ok && r.Header.Get("HX-Request") == "true" {
		return &Page{Status: http.StatusNoContent, HXRedirect: to}
	}
	return &Page{Status: http.StatusSeeOther, Location: to}
}

// LocalPath reports whether p is a path on this site and nothing else: no
// scheme, no authority, a leading slash, no backslash.
//
// It is the answer to "may I send a browser here", and there is one of it
// because there were two and one of them was wrong. The admin sign-in form
// checked a leading slash and a second character that was not a slash, so
// `next=/\evil.example` passed — and every browser resolves a slash followed by
// a backslash as an authority, so the review's login redirect left the site.
// modules/site had the correct rule for its navigation; this is that rule, in
// the kernel, and both callers read it.
//
// url.Parse is what tells the rest apart: a path on this site parses to no
// scheme, no authority and a path that begins with a slash. The two prefix
// checks come first because the parse alone is not enough — url.Parse reports
// "//evil.example" as an empty host and a path, and a browser collapsing the
// slashes goes to evil.example — and the backslash is refused outright, because
// it has no meaning in a path and its only use here is to look like something
// else.
func LocalPath(p string) bool {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.ContainsRune(p, '\\') {
		return false
	}
	u, err := url.Parse(p)
	return err == nil && u.Scheme == "" && u.Host == "" && strings.HasPrefix(u.Path, "/")
}

// SeeOther is a handler saying the answer is somewhere else: the row a write
// just created, or the list a delete just changed.
//
// It is an error because "not this page" is a return a handler already has:
// keeping it there is what lets every page have one signature.
type SeeOther string

func (s SeeOther) Error() string { return "see " + string(s) }

// Script is an inline <script> with this request's content security policy
// nonce on it. A page that must run something before the first paint uses it;
// every other script is a file under Static and needs nothing.
//
// It exists because the alternative is remembering: the policy in headers.go
// allows an inline script only with the nonce, so a tag written without one is
// dropped by the browser and reported in a console nobody is reading. This is
// the shape that cannot be written wrong.
func Script(ctx context.Context, js string) g.Node {
	return h.Script(g.Attr("nonce", NonceFrom(ctx)), g.Raw(js))
}

// pageErrors are the statuses a page can answer with when its caller declares
// none: the two a person can act on, and the one that says the database is not
// reachable.
var pageErrors = []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusServiceUnavailable}

// HTML mounts one page, with the authorization it declares, exactly like every
// other operation in the application. Hidden keeps it out of the OpenAPI
// document — a page is not an API — and the kernel records it anyway, so the
// boot gate still sees it. See this package's comment.
//
// The caller builds the operation, because what a page belongs to is the
// caller's to say: its tag, its summary, and the sign-in form an anonymous
// visitor is sent to (SignIn). What is not the caller's is the shape of the
// answer, which is why a SeeOther returned by the handler becomes the redirect
// here rather than in each of the seven screens that return one.
func HTML[I any](api *API, op huma.Operation, auth Auth, handler func(context.Context, *I) (*Page, error)) {
	op.Hidden = true
	if len(op.Errors) == 0 {
		op.Errors = pageErrors
	}
	Register(api, op, auth, func(ctx context.Context, in *I) (*Page, error) {
		out, err := handler(ctx, in)
		var to SeeOther
		if errors.As(err, &to) {
			return Redirect(ctx, string(to)), nil
		}
		return out, err
	})
}
