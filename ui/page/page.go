// Package page is how a shell turns a screen into an HTML document.
//
// It is the composition layer between ui/components and a module's pages, and
// it exists because it was written twice: modules/admin wrote a frame, a head,
// a mount wrapper and an error page, and the first client storefront copied all
// four. The copy drifted where it mattered — a stylesheet emitted twice, a
// script that named a route the shop did not have.
//
// The document itself is ui/document's: Chrome, View, Document, Render and the
// recovery notices are values and functions of values there, and this package
// aliases them so a shell that already says page.Chrome keeps saying it. What
// this package adds is the request: Request carries the typed tenant and
// principal a frame asks the Authorizer about, Serve reads it off the context
// and hands the rendered bytes to kit/httpx, and the three functions that need
// the kernel's rules — the local-path check on the sign-in link, the nonce on
// an inline script, the status text of a fault — apply them here before the
// pure document sees a value.
package page

import (
	"net/http"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/document"
)

// Chrome is what every page of one shell has in common. See document.Chrome.
type Chrome = document.Chrome

// View is one page's content. See document.View.
type View = document.View

// Request is what a render may know about the caller. Serve reads it once from
// the context so that every function below it is a function of values.
//
// It carries the kernel's own types — a frame asks the Authorizer about the
// tenant and shows the principal's roles — and projects to document.Request,
// which knows the tenant by name and the principal by identity, when the
// document is rendered.
type Request struct {
	// Path is the request's own path; a sidebar marks it current.
	Path string
	// Tenant is the one the host resolved to; the zero value when none did.
	Tenant tenancy.Tenant
	// Principal is who is calling, and SignedIn says whether anybody is.
	Principal tenancy.Principal
	SignedIn  bool
	// Locale is present when the shell composes Messages. It belongs to this
	// request, including its selected content language and x/text formatter.
	Locale *Locale
	// Inline are the nonce-bearing inline scripts this response may run, built
	// by Serve with InlineScript. A renderer cannot make one: it has no nonce.
	Inline []g.Node
}

// document is the request as the pure document reads it: the tenant's name,
// the signed-in principal's identity, and nothing an anonymous page could
// name somebody by.
func (r Request) document() document.Request {
	d := document.Request{Path: r.Path, Tenant: r.Tenant.Name, Locale: r.Locale, Inline: r.Inline}
	if r.SignedIn {
		d.Principal = r.Principal.UserID.String()
	}
	return d
}

// Document renders a whole HTML document: the head from the chrome and the
// view, and the framed body. It is document.Document with the kernel's rule
// applied first: a sign-in that is not a local path is no sign-in, so neither
// the recovery link nor data-signin can send a browser off this site.
func Document(c Chrome, r Request, v View, body g.Node) g.Node {
	return document.Document(localSignIn(c), r.document(), v, body)
}

// localSignIn is the chrome with a sign-in httpx.LocalPath refused cleared.
// Chrome.SignIn is a shell's constant, so this is defense in depth: the one
// rule for where a browser may be sent, read at the one place a document is
// built from a chrome.
func localSignIn(c Chrome) Chrome {
	if !httpx.LocalPath(c.SignIn) {
		c.SignIn = ""
	}
	return c
}

// RequestNoticeExamples captures the visible content Document serves for request
// failures, with the sign-in link present only for a local path. See
// document.RequestNoticeExamples for the captured contract.
func RequestNoticeExamples(signIn string) []examples.Example {
	if !httpx.LocalPath(signIn) {
		signIn = ""
	}
	return document.RequestNoticeExamples(signIn)
}

// Brand is what the page calls the installation: the tenant's name, or the
// chrome's when the tenant has none.
func Brand(c Chrome, r Request) string { return document.Brand(c, r.document()) }

// Bare is the frame with no navigation: a narrow column of cards.
func Bare(body []g.Node) g.Node { return document.Bare(body) }

// Fault is the page for a refusal a person can act on: the status text, the
// detail, and one way back. A 5xx never reaches it — see Serve.
func Fault(status int, detail, back, backLabel string) View {
	return document.Fault(status, http.StatusText(status), detail, back, backLabel)
}

// Empty is the input of a page that takes none. huma needs a type per shape.
type Empty struct{}
