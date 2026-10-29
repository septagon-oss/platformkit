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
	"github.com/septagon-oss/platformkit/ui/style"
)

// Chrome is what every page of one shell has in common.
type Chrome struct {
	// Brand is the name shown when the tenant has none: the document title's
	// suffix and the sidebar's label.
	Brand string
	// Assets is the prefix the shell serves ui.Assets under, e.g. "/app/admin/assets".
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
	// Revalidate says this body changes under its own address, so a cache may
	// hold it but must ask before answering from it. A page the owner can republish
	// is such a document: the default freshness of a public face is one minute, and
	// an owner who publishes and reloads would otherwise be shown the page they just
	// replaced — which reads as an installation that lost the write, not a browser
	// that kept the answer. Only the public face is cacheable to begin with, so this
	// says nothing about a workspace document, which is never stored at all.
	Revalidate bool
	// Modules are scripts this one page loads as ES modules, as paths under the
	// shell's asset prefix. A page with a rich text field names the editor; every
	// other page carries no tag and no bytes. It is a field of the view and not of
	// the chrome because the question is per page: the controllers a shell loads on
	// every page answer a different question, and a page weight budget is only real
	// if the answer is per page too.
	Modules []string
	// RunsWASM says this document instantiates WebAssembly, which the content
	// security policy refuses by default: 'self' and a nonce speak about where a
	// script came from, and a module built from bytes came from nowhere. The
	// adapter declares it for this response only (httpx.AllowWASM), which adds
	// 'wasm-unsafe-eval' to its script-src — an image editor or a trace view is
	// such a page, and a page that said nothing is served today's policy.
	// It says nothing about the markup: the body of a view that declares it and
	// the body of one that does not are the same bytes.
	RunsWASM bool
	// Live names the events this page's body is a region for — the topics a
	// connection is opened for, each one declared by the module that owns the
	// rows the body lists. It is the page's claim about its own body and nothing
	// else: the shell keeps no list, and a page that names none is drawn exactly
	// as it was before the field existed.
	//
	// The name is the browser's, not a new fact: the same strings are the SSE
	// event names the server sends and the names the module declared in its
	// manifest. Nothing here reads a topic's payload — the frame is a hint to
	// read again, so what the person sees is a fresh read of rows they may
	// already read, never what the frame carried.
	Live []string
	Head []g.Node
	Body []g.Node
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
		{"denied", "You don't have access", "Keep this page open to retain your input. Ask an administrator for the access this needs.", false},
		{"changed", "Account changed", "Sign in with the account that opened this page before submitting again. Keep this page open to retain your input.", true},
		{"uncertain", "We couldn't confirm whether your changes were saved.", "Keep this page open and check whether the action completed before trying again.", false},
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

// liveBefore and liveAfter are what a live region needs in the document besides
// the extension tag: htmx, which the extension registers itself with the moment
// it is evaluated, and the controller file whose live controller turns a frame
// into one refresh. A page that declares Live asks the kernel to draw a region,
// so the head gives the page what that region runs on rather than assuming the
// chrome thought of it: the chrome that carries neither is a real one (a light
// page with no controllers at all), and a document that loaded the extension
// alone threw on load and never opened its connection.
//
// htmx goes ahead of a chrome's own controllers, because the list a chrome gives
// runs in the order it is given and htmx-config.js configures a global that has
// to exist first; components.js goes after them, because it is the file that
// reads what the others configure. A file the chrome already lists is never
// handed to it twice.
var (
	liveBefore = []string{"htmx.min.js"}
	liveAfter  = []string{"components.js"}
)

// head is every page's head: the stylesheet with its fingerprint, the inline
// snippets, the controllers as deferred scripts in order — deferred, so the
// document parses before any of them runs and the order is still theirs — and
// whatever the view adds.
func head(c Chrome, r Request, v View) g.Node {
	listed := c.Scripts
	if len(v.Live) > 0 {
		listed = liveScripts(c.Scripts)
	}
	scripts := make([]g.Node, 0, len(listed))
	for _, name := range listed {
		scripts = append(scripts, h.Script(h.Src(c.Assets+"/js/"+name), g.Attr("defer")))
	}
	// A module is fetched with the document it belongs to and never deferred: it
	// is the page's own enhancement, and the page it enhances is already parsed by
	// the time a module at the end of the head runs.
	modules := make([]g.Node, 0, len(v.Modules))
	for _, path := range v.Modules {
		modules = append(modules, h.Script(h.Src(c.Assets+"/"+path), g.Attr("type", "module")))
	}
	// A live region is bound by the pinned extension, and only a page that names
	// a topic pays for it. It is a module script rather than a deferred classic
	// one because module scripts and deferred scripts share the one in-order list
	// that runs before DOMContentLoaded, which is when htmx reads the document —
	// so the extension is defined before any element is read, with no coordination
	// between the two files and no inline script the policy would have to forgive.
	if len(v.Live) > 0 {
		modules = append(modules, h.Script(h.Src(c.Assets+"/js/"+ui.SSEExtension), g.Attr("type", "module")))
	}
	return h.Head(
		h.Meta(h.Charset("utf-8")),
		h.Meta(h.Name("viewport"), h.Content("width=device-width, initial-scale=1")),
		h.Meta(h.Name("color-scheme"), h.Content("light dark")),
		h.TitleEl(g.Text(v.Title+" · "+Brand(c, r))),
		h.Link(h.Rel("stylesheet"), h.Href(c.Assets+"/app.css?v="+c.Stylesheet.Fingerprint)),
		g.If(v.Theme == "", g.Group(r.Inline)),
		g.Group(scripts),
		g.Group(modules),
		g.Group(v.Head),
	)
}

// liveScripts is the chrome's own controller list with whatever a live region
// still runs on added around it, in the order the runtime needs.
func liveScripts(listed []string) []string {
	out := make([]string, 0, len(listed)+len(liveBefore)+len(liveAfter))
	for _, name := range liveBefore {
		if !slices.Contains(listed, name) {
			out = append(out, name)
		}
	}
	out = append(out, listed...)
	for _, name := range liveAfter {
		if !slices.Contains(listed, name) {
			out = append(out, name)
		}
	}
	return out
}

// Brand is what the page calls the installation: the tenant's name, or the
// chrome's when the tenant has none.
func Brand(c Chrome, r Request) string {
	if strings.TrimSpace(r.Tenant) != "" {
		return r.Tenant
	}
	return c.Brand
}

// Bare is the access-page frame: an inset, narrow main landmark without navigation.
func Bare(body []g.Node) g.Node {
	return components.Standalone(components.ComponentProps{}, body...)
}

// Fault is the page for a refusal a person can act on: the status text, the
// detail, and one way back. title is what the status is called — the caller words it,
// because this package links no net/http and holds no copy of its own.
//
// It is five positional strings rather than FaultProps for one reason: the affordances
// a person may act on grew into the props struct, and the page that has only the way
// back stays the simplest thing to call. It is the same document either way — this is
// FaultPage with one field filled.
func Fault(status int, title, detail, back, backLabel string) View {
	return FaultPage(FaultProps{Status: status, Title: title, Sentence: detail, Way: back, WayLabel: backLabel})
}

// ToneInfo is the quiet voice: an outcome the application reached by working as
// designed — a feature the plan does not carry — rather than one that broke.
const ToneInfo = "info"

// FaultProps is the generic verdict page: what happened, in one line, how loudly it is
// announced, and every way on that actually exists for this request.
//
// It is a props struct rather than five positional strings for the reason 0015
// gives for RefusalProps: the affordances are conditional, and a page drawn from
// five strings in a row cannot leave one out without the reader of the call
// knowing which one meant "nothing". Every field here is optional, and a field
// that is empty renders no control — the page never offers a door nobody mounted.
// Every string arrives already worded by the caller, in the request's language.
type FaultProps struct {
	Status int
	// Title is what the status is called — ui/page passes http.StatusText, which
	// this package does not link net/http to say.
	Title string
	// Sentence is the verdict's own line, code first, as every refusal shows it.
	Sentence string
	// Tone is "" for a fault, announced as an alert, or ToneInfo for a verdict that
	// is not a fault, announced politely and drawn without the alarm colour.
	Tone string
	// Retry is the request's own address, offered for a verdict that clears by
	// waiting and only on a method a link can re-issue. RetryAfter is the guard's
	// own Retry-After in whole seconds, and 0 says no guard sent one — which the
	// page says nothing about rather than inventing a wait.
	Retry, RetryLabel string
	RetryAfter        int
	RetryAfterLabel   string
	// Back is the page the visitor came from, and only ever a path on this site;
	// ui/page checks that before it gets here.
	Back, BackLabel string
	// Way is the way on the composition owns — the one link Fault offers. It is
	// never the address that refused: Retry already is that, deliberately, and it
	// appears above Way so a person who could simply wait is not offered the loss of
	// their place instead.
	Way, WayLabel string
	// SignIn is the chrome's own sign-in address, offered only on the verdict
	// signing in could change.
	SignIn, SignInLabel string
	// Reference is the request's own identifier — the one an operator finds the log
	// line by — and ReferenceLabel the words that name it, which the shell's
	// catalogue provides. Code is the refusal's own code, carried here where the
	// caller wrote the person's sentence in place of the guard's line and the code
	// would otherwise have left the page. All three are "" where there is nothing
	// to show, and one empty identifier leaves the line off the page.
	Reference, ReferenceLabel, Code string
}

// FaultPage is the refusal document a person may be able to act on: the verdict's
// name, its sentence, and every way on that exists for this request. A field that is
// empty renders no control, so the page never offers a door nobody mounted.
func FaultPage(p FaultProps) View {
	if strings.TrimSpace(p.Sentence) == "" {
		p.Sentence = "That did not work."
	}
	body := []g.Node{
		components.Toolbar(components.ToolbarProps{Title: p.Title}),
		refusalLine(p.Sentence, p.Tone),
	}
	if p.Retry != "" {
		body = append(body, components.Link(components.LinkProps{Label: p.RetryLabel, Href: p.Retry}))
	}
	if p.RetryAfter > 0 {
		body = append(body, refusalSentence(p.RetryAfterLabel, "secondary"))
	}
	for _, way := range []struct{ href, label string }{{p.Back, p.BackLabel}, {p.Way, p.WayLabel}, {p.SignIn, p.SignInLabel}} {
		if way.href != "" {
			body = append(body, components.Link(components.LinkProps{Label: way.label, Href: way.href}))
		}
	}
	if p.Reference != "" || p.Code != "" {
		body = append(body, refusalReference(p.ReferenceLabel, p.Reference, p.Code))
	}
	return View{Title: p.Title, Status: p.Status, Language: "en", Body: body}
}

// clRefusalMeasure bounds the sentence of a refusal page to the measure the rest of
// the frame's prose keeps. The design floor divides a paragraph's box by half its
// font size, and a refusal whose body is the one line a person came to read spans the
// whole content column: 142 characters at 1440 px. The bound never centres: the
// sentence keeps the left edge the title above it sits on.
var clRefusalMeasure = style.New().MaxWScaled(style.MaxWProse)

// refusalLine is the verdict's own sentence, in the frame's content column.
//
// The Alert component is right for a notice that arrives beside other content —
// that is what its icon column, its padding and its live region are for — and wrong
// for the page whose whole body is one refusal: the frame's border and padding and
// the icon push the one line a person came to read some four centimetres off the
// column every other line on the page sits on, which is a third alignment on a page
// that speaks two. The announcement stays: this is the page a person is told the
// refusal on, and it says so as an alert the way the component says it — except for a
// verdict drawn in the info tone, which announces itself politely because nothing
// broke and a screen reader should not interrupt for it.
func refusalLine(sentence, tone string) g.Node {
	role, color := "alert", "danger"
	if tone == ToneInfo {
		role, color = "status", ""
	}
	return h.Div(h.Role(role), refusalSentence(sentence, color))
}

// refusalSentence is one line of a refusal page in the frame's own paragraph, bounded
// to the measure the floor reads.
func refusalSentence(sentence, color string) g.Node {
	p := components.TextProps{Content: sentence, Element: "p", Color: color}
	p.Class = clRefusalMeasure.Compile()
	return components.Text(p)
}

// refusalReference is the identifier line under a verdict. The identifier is drawn as
// a value a person can select and read back digit for digit, under the words that name
// it, and the code follows beside it rather than inside either sentence: the label is a
// translator's, the identifier a row in a log, the code a grep pattern, so they are
// separate strings on the page and not one line no translation can finish. The line
// keeps the measure the verdict's own sentence keeps — the person reads both, and a
// second line that spans the column is the same defect one paragraph lower.
func refusalReference(label, reference, code string) g.Node {
	var parts []g.Node
	if reference != "" {
		if label != "" {
			parts = append(parts,
				components.Text(components.TextProps{
					Content: label, Element: "span", Color: "muted", Size: "sm"}),
				g.Text(" "))
		}
		parts = append(parts, components.Text(components.TextProps{
			Content: reference, Element: "samp", Size: "sm", NoWrap: true}))
	}
	if code != "" {
		if len(parts) > 0 {
			parts = append(parts, g.Text(" · "))
		}
		parts = append(parts, h.Code(g.Text(code)))
	}
	return h.P(h.Class(clRefusalMeasure.Compile()), g.Group(parts))
}

// refusalReferenceInSentence is the same identifier on the page that answers the grant
// question: there the refusal's own line names the permission and the person who can
// give it, and the reference closes that sentence rather than standing under a label of
// its own. It keeps the measure refusalReference keeps, for the same reason.
func refusalReferenceInSentence(words, reference string) g.Node {
	return h.P(h.Class(clRefusalMeasure.Compile()),
		g.Text(words), g.Text(" "), h.Code(g.Text(reference)), g.Text(")"))
}

// RefusalProps is the one verdict that arrives with parts: a refusal of a
// person who could be told their way out. Fault draws an alert and a link, and
// that is every other verdict needs; a person who was refused a grant needs four
// things — what they lack, in words, who can hand it to them, a door that
// answers, and a way to ask — and a page that offers only the last one is the
// dead end the walkthrough met.
//
// Every string arrives already worded, by the caller, in the request's language:
// this package holds no copy of its own and negotiates nothing, which is why a
// storefront and a refusal page can be drawn by the same layer.
type RefusalProps struct {
	Status int
	// Title is what the status is called, as in Fault.
	Title string
	// Sentence is the refusal's own line, code first, as every refusal shows it.
	Sentence string
	// Missing names the grant in words ("What is missing: read tasks"), and
	// Granter names who may hand it out — a role, never a list of people.
	Missing, Granter string
	// Home is the way on. It is never the address that refused: a page whose one
	// link leads back to the same refusal is the defect this page exists to fix.
	Home, HomeLabel string
	// Ask is the address the ask posts to, and "" leaves no ask control on the
	// page — a button to a door nobody mounted is a lie about a door.
	Ask, AskLabel, Permission, Path string
	// SignIn is the chrome's own sign-in address, offered only on the verdict
	// signing in could change (a 401), and "" leaves no link on the page — the
	// same rule Ask follows. The refusal page is unchanged by everything
	// below it: the fields are empty there, and an empty field renders nothing.
	SignIn, SignInLabel string
	// Retry, RetryAfter and the label beside it are the transient verdict's way on,
	// on the two shapes of the one verdict: a refusal page for a 503 is still the
	// refusal page, and it still has to say you may wait.
	Retry, RetryLabel string
	RetryAfter        int
	RetryAfterLabel   string
	// Reference is the request's identifier, as in FaultProps. The words that name it
	// arrive as ReferenceWords because on this page they open the clause the
	// identifier closes rather than labelling a line of its own.
	Reference, ReferenceWords string
}

// Refusal is the page for a person the kernel refused before any handler ran.
func Refusal(p RefusalProps) View {
	if strings.TrimSpace(p.Sentence) == "" {
		p.Sentence = "That did not work."
	}
	body := []g.Node{
		components.Toolbar(components.ToolbarProps{Title: p.Title}),
		refusalLine(p.Sentence, ""),
	}
	if p.Missing != "" {
		body = append(body, refusalSentence(p.Missing, ""))
	}
	if p.Granter != "" {
		body = append(body, refusalSentence(p.Granter, "secondary"))
	}
	if p.Ask != "" {
		body = append(body, components.Form(components.FormProps{Action: p.Ask, Label: p.AskLabel},
			h.Input(h.Type("hidden"), h.Name("permission"), h.Value(p.Permission)),
			h.Input(h.Type("hidden"), h.Name("path"), h.Value(p.Path)),
			components.Button(components.ButtonProps{Label: p.AskLabel, Type: "submit", Size: "md"}),
		))
	}
	if p.Retry != "" {
		body = append(body, components.Link(components.LinkProps{Label: p.RetryLabel, Href: p.Retry}))
	}
	if p.RetryAfter > 0 {
		body = append(body, refusalSentence(p.RetryAfterLabel, "secondary"))
	}
	if p.SignIn != "" {
		body = append(body, components.Link(components.LinkProps{Label: p.SignInLabel, Href: p.SignIn}))
	}
	if p.Home != "" {
		body = append(body, components.Link(components.LinkProps{Label: p.HomeLabel, Href: p.Home}))
	}
	if p.Reference != "" {
		body = append(body, refusalReferenceInSentence(p.ReferenceWords, p.Reference))
	}
	return View{Title: p.Title, Status: p.Status, Language: "en", Body: body}
}
