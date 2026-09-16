// Package document is an HTML document as values: what every page of a shell
// shares (Chrome), what a render may know about the caller (Request), what a
// handler returned (View), and Document, which puts them together around the
// body a frame arranged. Render turns the result into bytes.
//
// Nothing here reads a context, a request or a database. The nonce an inline
// script needs and the tenant a page is served for arrive as values; ui/page
// is the adapter that reads them off the request and hands the response to
// kit/httpx. That is what lets a storefront, a mail preview or a test render
// the same document without linking the router or the storage adapter, and
// it is why the package gate holds this package to design, ui/css, ui/style,
// ui/components, ui/components/examples, ui, kit/locale and gomponents.
package document

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
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
	//
	// It is a path on this site and nothing else. This package renders what it
	// is given; ui/page checks the value against httpx.LocalPath, the one rule
	// for where a browser may be sent, and clears anything that fails it.
	SignIn string
	// Theme pins data-theme on <html>: a shop is the brand's colour and has no
	// toggle. Empty follows the operating system and the person's own toggle,
	// applied before first paint by an inline snippet the adapter adds.
	Theme string
	// Attrs are further <html> attributes: a client's grain and scrollbar,
	// which the shell's own rules give a meaning to. A lang attribute sets the
	// shell's default document language; View.Language takes precedence.
	Attrs map[string]string
}

// Request is what a document may know about the caller, as plain values. The
// adapter reads them once from the request context; ui/page.Request carries
// the typed tenant and principal a frame asks the Authorizer about, and
// projects them to this before the document is rendered.
type Request struct {
	// Path is the request's own path; a sidebar marks it current.
	Path string
	// Tenant is the display name of the tenant the host resolved to, empty
	// when none did. Brand falls back to the chrome's.
	Tenant string
	// Principal identifies the signed-in caller to the page's own controllers,
	// as the data-principal attribute a form is pinned to. Empty means
	// anonymous, and an anonymous document names nobody.
	Principal string
	// Locale is present when the shell composes Messages: this request's
	// selected content language and formatter.
	Locale *locale.Locale
	// Inline are the nonce-bearing inline scripts this response may run, built
	// by the adapter with InlineScript. A renderer cannot make one: it has no
	// nonce.
	Inline []g.Node
}

// View is one page's content. Status zero is 200. Bare asks for the frame with
// no navigation — the sign-in screen, shown to somebody who has none yet.
type View struct {
	Title  string
	Status int
	Bare   bool
	// Sensitive prevents caching and sends no referrer on this document's
	// outgoing requests, including assets loaded before scripts run. Use it
	// for pages carrying password links or other private URL credentials.
	// It does not redact the incoming URL from application or proxy logs.
	Sensitive bool
	// Language is the BCP 47 tag of the rendered copy, such as "pt-PT".
	// Empty uses Chrome.Attrs["lang"], or English when no default is set.
	// The application selects and translates the content; this field only
	// declares its language to the browser and assistive technology.
	Language string
	// Theme pins data-theme on this document over the chrome's, for a page
	// whose theme is the tenant's choice rather than the shell's. A pinned
	// document carries no theme script: the choice is not the visitor's.
	Theme string
	Head  []g.Node
	Body  []g.Node
}

// Document renders a whole HTML document: the head from the chrome and the
// view, and the framed body. It is pure; body is the frame's result.
func Document(c Chrome, r Request, v View, body g.Node) g.Node {
	keys := slices.Sorted(maps.Keys(c.Attrs))
	language := cmp.Or(v.Language, c.Attrs["lang"])
	if language == "" {
		for _, k := range keys {
			if strings.EqualFold(k, "lang") && c.Attrs[k] != "" {
				language = c.Attrs[k]
				break
			}
		}
	}
	attrs := []g.Node{h.Lang(cmp.Or(language, "en"))}
	if theme := cmp.Or(v.Theme, c.Theme); theme != "" {
		attrs = append(attrs, g.Attr("data-theme", theme))
	}
	if c.SignIn != "" {
		attrs = append(attrs, g.Attr("data-signin", c.SignIn))
	}
	if r.Principal != "" {
		attrs = append(attrs, g.Attr("data-principal", r.Principal))
	}
	for _, k := range keys {
		if !strings.EqualFold(k, "lang") {
			attrs = append(attrs, g.Attr(k, c.Attrs[k]))
		}
	}
	return h.HTML(append(attrs, head(c, r, v), h.Body(body, requestNotices(c), sessionNotice(c)))...)
}

// The notice stays outside account menus, which may close during sign-out.
func sessionNotice(c Chrome) g.Node {
	if !slices.Contains(c.Scripts, "session.js") {
		return nil
	}
	return components.Alert(components.AlertProps{
		ComponentProps: components.ComponentProps{Hidden: true, Attrs: map[string]string{
			"data-session-error": "", "lang": "en", "tabindex": "-1",
		}},
		Tone: "danger", Bordered: true,
	})
}

// Notices are source-rendered components, not HTML rebuilt by a controller.
// They stay outside the form's swap target; its input is never serialized.
func requestNotices(c Chrome) g.Node {
	if !slices.Contains(c.Scripts, "htmx-config.js") {
		return nil
	}
	var nodes []g.Node
	for _, example := range RequestNoticeExamples(c.SignIn) {
		nodes = append(nodes, h.Div(h.ID(example.ID), h.Hidden(""), h.Lang("en"), g.Attr("data-request-notice", ""), example.Node))
	}
	return g.Group(nodes)
}

// RequestNoticeExamples captures the visible content Document serves for request
// failures. IDs retain the controller's existing notice identities; Stack, Alert
// and Link keep their shared typed interfaces. The hidden, English-language DOM
// wrapper remains Document's responsibility. A consumer may include these values
// in its own export or composition without adding a second notice catalog.
// These are presentation candidates, not request classification, automatic retry,
// source persistence or an executable prototype.
//
// signIn is rendered as the recovery link when it is not empty; it is the
// caller's to have checked. ui/page.RequestNoticeExamples applies the kernel's
// local-path rule before calling this, so a shell reaches the rule through it.
func RequestNoticeExamples(signIn string) []examples.Example {
	var captures []examples.Example
	for _, notice := range []struct {
		kind, title, message string
		signin               bool
	}{
		{"anonymous", "Sign-in required", "Keep this page open to retain your input. Sign in in another tab, then return and try again. Nothing is retried automatically.", true},
		{"denied", "Permission denied", "You do not have permission for this action. Keep this page open to retain your input and contact an administrator if you need access.", false},
		{"changed", "Account changed", "Sign in with the account that opened this page before submitting again. Keep this page open to retain your input.", true},
		{"uncertain", "Check the result", "The request outcome is unknown. Keep this page open and check whether the action completed before trying again.", false},
	} {
		body := []g.Node{examples.ExampleWithSlots(
			examples.ExampleInfo{ID: "message", ComponentID: "pk-ui.component.alert"},
			components.AlertProps{Tone: "danger", Title: notice.title, Message: notice.message, Bordered: true},
			components.AlertSlots{}, components.AlertWithSlots).Node}
		if notice.signin && signIn != "" {
			body = append(body, examples.ExampleOf(
				examples.ExampleInfo{ID: "sign-in", ComponentID: "pk-ui.component.link"},
				components.LinkProps{Label: "Sign in (opens a new tab)", Href: signIn, External: true}, components.Link).Node)
		}
		captures = append(captures, examples.ExampleWithChildren(
			examples.ExampleInfo{ID: "pk-auth-" + notice.kind, ComponentID: "pk-ui.component.stack", Group: "Request recovery", Name: notice.title},
			components.StackProps{Gap: "3"}, body, components.Stack))
	}
	return captures
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
		g.If(v.Theme == "", g.Group(r.Inline)),
		g.Group(scripts),
		g.Group(v.Head),
	)
}

// Brand is what the page calls the installation: the tenant's name, or the
// chrome's when the tenant has none.
func Brand(c Chrome, r Request) string {
	if strings.TrimSpace(r.Tenant) != "" {
		return r.Tenant
	}
	return c.Brand
}

// Bare is the frame with no navigation: a narrow column of cards.
func Bare(body []g.Node) g.Node {
	return components.Container(components.ContainerProps{MaxWidth: "sm"},
		components.Stack(components.StackProps{Gap: "6"}, body...))
}

// Fault is the page for a refusal a person can act on: the status text, the
// detail, and one way back. title is what the status is called — ui/page
// passes http.StatusText, which this package does not link net/http to say.
func Fault(status int, title, detail, back, backLabel string) View {
	if strings.TrimSpace(detail) == "" {
		detail = "That did not work."
	}
	return View{Title: title, Status: status, Language: "en", Body: []g.Node{
		components.Toolbar(components.ToolbarProps{Title: title}),
		components.Alert(components.AlertProps{Tone: "danger", Message: detail, Bordered: true}),
		components.Link(components.LinkProps{Label: backLabel, Href: back}),
	}}
}
