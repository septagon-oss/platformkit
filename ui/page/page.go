// Package page is how a shell turns a screen into an HTML document.
//
// It is the composition layer between ui/components and a module's pages, and
// it exists because it was written twice: modules/admin wrote a frame, a head,
// a mount wrapper and an error page, and the first client storefront copied all
// four. The copy drifted where it mattered — a stylesheet emitted twice, a
// script that named a route the shop did not have.
//
// Everything here is a value or a function of values. Chrome is what every page
// of a shell shares, built once at mount. Request is what a render may know
// about the caller, read once by Serve. View is what a handler returns. Frame
// is how a shell arranges a body: the admin's sidebar, the shop's bar. Document
// puts them together. The one place that reads a context.Context or writes a
// response is Serve.
package page

import (
	"maps"
	"net/http"
	"slices"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
)

// Chrome is what every page of one shell has in common.
type Chrome struct {
	// Brand is the name shown when the tenant has none: the document title's
	// suffix and the sidebar's label.
	Brand string
	// Assets is the prefix the shell serves ui.Assets under, e.g. "/admin/assets".
	Assets string
	// Stylesheet is the sheet the shell composed, once, with ui.Compose.
	Stylesheet ui.Sheet
	// Scripts are the controllers a page loads, in order, as file names under
	// Assets + "/js/". A shell lists the kernel's it wants and its own.
	Scripts []string
	// SignIn is where an anonymous visitor to a guarded page is sent, and the
	// route offered for recovery without leaving an unsaved form. Empty means
	// the shell has no sign-in link. Controllers never invent a route.
	SignIn string
	// Theme pins data-theme on <html>: a shop is the brand's colour and has no
	// toggle. Empty follows the operating system and the person's own toggle,
	// applied before first paint by an inline snippet Serve adds.
	Theme string
	// Attrs are further <html> attributes: a client's grain and scrollbar,
	// which the shell's own rules give a meaning to.
	Attrs map[string]string
}

// Request is what a render may know about the caller. Serve reads it once from
// the context so that every function below it is a function of values.
type Request struct {
	// Path is the request's own path; a sidebar marks it current.
	Path string
	// Tenant is the one the host resolved to; the zero value when none did.
	Tenant tenancy.Tenant
	// Principal is who is calling, and SignedIn says whether anybody is.
	Principal tenancy.Principal
	SignedIn  bool
	// Inline are the nonce-bearing inline scripts this response may run, built
	// by Serve with httpx.Script. A renderer cannot make one: it has no nonce.
	Inline []g.Node
}

// View is one page's content. Status zero is 200. Bare asks for the frame with
// no navigation — the sign-in screen, shown to somebody who has none yet.
type View struct {
	Title  string
	Status int
	Bare   bool
	Head   []g.Node
	Body   []g.Node
}

// Document renders a whole HTML document: the head from the chrome and the
// view, and the framed body. It is pure; body is the frame's result.
func Document(c Chrome, r Request, v View, body g.Node) g.Node {
	attrs := []g.Node{h.Lang("en")}
	if c.Theme != "" {
		attrs = append(attrs, g.Attr("data-theme", c.Theme))
	}
	if c.SignIn != "" {
		attrs = append(attrs, g.Attr("data-signin", c.SignIn))
	}
	if r.SignedIn {
		attrs = append(attrs, g.Attr("data-principal", r.Principal.UserID.String()))
	}
	for _, k := range slices.Sorted(maps.Keys(c.Attrs)) {
		attrs = append(attrs, g.Attr(k, c.Attrs[k]))
	}
	return h.HTML(append(attrs, head(c, r, v), h.Body(body, requestNotices(c)))...)
}

// Notices are source-rendered components, not HTML rebuilt by a controller.
// They stay outside the form's swap target; its input is never serialized.
func requestNotices(c Chrome) g.Node {
	if !slices.Contains(c.Scripts, "htmx-config.js") {
		return nil
	}
	var nodes []g.Node
	for _, notice := range []struct {
		kind, title, message string
		signin               bool
	}{
		{"anonymous", "Sign-in required", "Keep this page open to retain your input. Sign in in another tab, then return and try again. Nothing is retried automatically.", true},
		{"denied", "Permission denied", "You do not have permission for this action. Keep this page open to retain your input and contact an administrator if you need access.", false},
		{"changed", "Account changed", "Sign in with the account that opened this page before submitting again. Keep this page open to retain your input.", true},
		{"uncertain", "Check the result", "The request outcome is unknown. Keep this page open and check whether the action completed before trying again.", false},
	} {
		nodes = append(nodes, h.Div(h.ID("pk-auth-"+notice.kind), h.Hidden(""), g.Attr("data-request-notice", ""),
			components.Stack(components.StackProps{Gap: "3"},
				components.Alert(components.AlertProps{Tone: "danger", Title: notice.title, Message: notice.message, Bordered: true}),
				g.If(notice.signin && httpx.LocalPath(c.SignIn), components.Link(components.LinkProps{
					Label: "Sign in (opens a new tab)", Href: c.SignIn, External: true})),
			)))
	}
	return g.Group(nodes)
}

// head is every page's head: the stylesheet with its fingerprint, the inline
// snippets, the controllers as deferred scripts in order — deferred, so the
// document parses before any of them runs and the order is still theirs — and
// whatever the view adds.
func head(c Chrome, r Request, v View) g.Node {
	scripts := make([]g.Node, 0, len(c.Scripts))
	for _, name := range c.Scripts {
		scripts = append(scripts, h.Script(h.Src(c.Assets+"/js/"+name), g.Attr("defer")))
	}
	return h.Head(
		h.Meta(h.Charset("utf-8")),
		h.Meta(h.Name("viewport"), h.Content("width=device-width, initial-scale=1")),
		h.Meta(h.Name("color-scheme"), h.Content("light dark")),
		h.TitleEl(g.Text(v.Title+" · "+Brand(c, r))),
		h.Link(h.Rel("stylesheet"), h.Href(c.Assets+"/app.css?v="+c.Stylesheet.Fingerprint)),
		g.Group(r.Inline),
		g.Group(scripts),
		g.Group(v.Head),
	)
}

// Brand is what the page calls the installation: the tenant's name, or the
// chrome's when the tenant has none.
func Brand(c Chrome, r Request) string {
	if strings.TrimSpace(r.Tenant.Name) != "" {
		return r.Tenant.Name
	}
	return c.Brand
}

// Bare is the frame with no navigation: a narrow column of cards.
func Bare(body []g.Node) g.Node {
	return components.Container(components.ContainerProps{MaxWidth: "sm"},
		components.Stack(components.StackProps{Gap: "6"}, body...))
}

// Fault is the page for a refusal a person can act on: the status text, the
// detail, and one way back. A 5xx never reaches it — see Serve.
func Fault(status int, detail, back, backLabel string) View {
	if strings.TrimSpace(detail) == "" {
		detail = "That did not work."
	}
	return View{Title: http.StatusText(status), Status: status, Body: []g.Node{
		components.Toolbar(components.ToolbarProps{Title: http.StatusText(status)}),
		components.Alert(components.AlertProps{Tone: "danger", Message: detail, Bordered: true}),
		components.Link(components.LinkProps{Label: backLabel, Href: back}),
	}}
}

// Empty is the input of a page that takes none. huma needs a type per shape.
type Empty struct{}
