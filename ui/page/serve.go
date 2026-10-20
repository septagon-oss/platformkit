package page

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	kitfault "github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/document"
)

// Frame arranges a view's body inside a shell: the admin's sidebar and header
// around a main, the shop's bar. It takes the context because a sidebar asks
// the Authorizer, and that is a query.
type Frame func(ctx context.Context, r Request, body []g.Node) g.Node

// Shell is what Serve needs from the shell that mounts a page: its chrome, its
// frame, the tag its operations carry, and the way back from an error page.
type Shell struct {
	Chrome    Chrome
	Frame     Frame
	Tag       string
	Back      string
	BackLabel string
	// BackKey names the catalogue entry BackLabel is worded under, so the way out
	// of a refusal arrives in the reader's language and not in the language whoever
	// wrote the shell happens to speak. It is named by the composition that ships
	// the copy for it, the same way a module's refusal names its own sentence
	// (kit/fault.Reason): one label, one key, and no two shells sharing words they
	// did not write. Empty, and the label is shown as written.
	BackKey string
	// Messages is composed once from the participating modules' catalogs.
	// Nil keeps the existing untranslated shell. Do not mutate it while serving.
	Messages Messages
	// Locale chooses an explicit URL, account or tenant preference. Empty or
	// unsupported values fall back to Accept-Language and the catalog default.
	// The application owns preference persistence and locale-preserving links.
	Locale func(context.Context, Request) string

	// Granter names who may hand out a missing permission, in the only terms a
	// refusal may name: a role, never a person. The permission is the auth
	// module's fact (which grant gates role management) and the Label its words;
	// ui/page renders both and decides neither. The zero value names no gate and
	// the refusal page says nothing about who could grant it.
	Granter Granter

	// Ask is the address an "Ask for access" form posts to, as the composition
	// mounted it (ui/page.MountAccess mounts the two page routes at this one
	// address and its confirmation). Empty leaves no ask control on a refusal: a
	// button whose action nobody mounted is a lie about a door.
	Ask string
}

// Granter is the shell's answer to "who can give me this".
type Granter struct{ Permission, Label string }

// Route is what one page is: its operation id, method, path, summary, and the
// statuses it may answer with beyond the kernel's defaults.
type Route struct {
	ID, Method, Path, Summary string
	Errors                    []int
	// StreamsBody marks the page that reads the request itself — a person
	// submitting a file, and nothing else in this repository today — so that the
	// kernel bounds that request by the deployment's upload ceiling rather than by
	// the megabyte every other one gets, and commits the guard's transaction before
	// the bytes start arriving rather than holding it open while they trickle in.
	// See httpx.StreamedBody. A page that sets it declares no body field: nothing
	// above it is going to decode one.
	StreamsBody bool
}

// Handler is a page. It reads through ctx and knows the caller through r; what
// it returns is a View, and the three errors it may return are a
// httpx.SeeOther, a *problem.Problem the caller can act on, or anything else,
// which is a 500 and the kernel's problem document.
type Handler[I any] func(ctx context.Context, r Request, in *I) (View, error)

// beforePaint applies the theme a person chose before the stylesheet does. It
// is inline because a deferred script flashes the wrong theme; it carries the
// request's nonce because inline is what the content security policy forbids
// without one. A chrome that pins a theme has no toggle and gets no snippet.
const beforePaint = `try{var t=localStorage.getItem("platformkit-theme");if(t)document.documentElement.setAttribute("data-theme",t)}catch(e){}`

// Serve mounts one page as an operation, exactly like every JSON route: the
// same recording, the same authorization declaration, the same transaction. It
// is the edge: it reads the request into a Request, renders the View through
// the frame, turns a SeeOther into the redirect and a 4xx problem into the same
// refusal page a kernel guard renders — worded through the locale this shell
// negotiated — and lets a 5xx keep the kernel's problem document and log line.
func Serve[I any](r *httpx.Router, s Shell, rt Route, auth httpx.Auth, handler Handler[I]) {
	if s.Messages != nil {
		_ = SelectLocale(s.Messages) // validate the provider at composition
	}
	op := huma.Operation{
		OperationID: rt.ID, Method: rt.Method, Path: rt.Path, Summary: rt.Summary,
		Tags: []string{s.Tag}, Errors: rt.Errors,
	}
	if rt.StreamsBody {
		key, streamed := httpx.StreamedBody()
		op.Extensions = map[string]any{key: streamed}
	}
	if s.Chrome.SignIn != "" {
		httpx.SignIn(&op, s.Chrome.SignIn)
	}
	httpx.HTML(r, op, auth, func(ctx context.Context, in *I) (*httpx.Page, error) {
		r := s.Request(ctx)
		v, err := handler(ctx, r, in)
		if err != nil {
			if to, ok := errors.AsType[httpx.SeeOther](err); ok {
				out := httpx.Redirect(ctx, string(to))
				applyPrivacy(out, v.Sensitive)
				return out, nil
			}
			status, detail, reason := refusal(err)
			if status >= http.StatusInternalServerError {
				return nil, err
			}
			// The refusal of a handler is the same page the kernel's guards render
			// (fault.go) and is worded the same way: through the locale this shell just
			// negotiated, with the sentence this shell ships for that verdict. The
			// refusal's own sentence travels with it (`reason`) when the writer named
			// one, which is what lets a module's rule be answered in the reader's
			// language without this package learning what the rule was; `fault` claims a
			// language only over a sentence it replaced, so a verdict no catalogue
			// speaks stays English and says so.
			refused := fault(status, detail, reason, r.Locale, "", s.Back, s.backLabel(r.Locale))
			refused.Sensitive = v.Sensitive
			v = refused
		}
		if r.Locale != nil {
			if v.Language == "" {
				v.Language = r.Locale.Language
			} else if v.Language != r.Locale.Language {
				r.Locale = new(SelectLocale(s.Messages, v.Language))
			}
		}
		status := v.Status
		if status == 0 {
			status = http.StatusOK
		}
		// A live region re-reads its own address when the server tells it that its
		// state moved, and the answer to that read is the region's contents and
		// nothing else: htmx puts these bytes inside the element the region already
		// is, so a document here would put a page inside a page and repeat the element
		// holding the connection. The rows behind the two answers come from the same
		// guarded read on the same route — the header changes the shape of an answer
		// and none of its content — and a page that named no topic is answered as a
		// whole document whatever the request carried, because nothing said it had a
		// region to swap.
		var out *httpx.Page
		if len(v.Live) > 0 && regionRequested(ctx) {
			out, err = RenderFragment(g.Group(v.Body), status)
		} else {
			region := v.Body
			if len(v.Live) > 0 {
				region = []g.Node{document.LiveRegion(streamConnect(v.Live), regionAddress(ctx, r), v.Live, v.Body)}
			}
			var body g.Node
			if v.Bare {
				body = Bare(region)
			} else {
				body = s.Frame(ctx, r, region)
			}
			out, err = Render(Document(s.Chrome, r, v, body), status)
		}
		if err != nil {
			return nil, err
		}
		if s.Messages != nil {
			out.ContentLanguage, out.Vary = v.Language, "Accept-Language"
			// A preference resolver may depend on the signed-in account, so the
			// safe answer is that this response belongs to one person. On the
			// public surface there is no account to read — the chain parses no
			// session there at all — so the language can only have come from the
			// request's own Accept-Language, and what Vary says is the truth:
			// the cache must key on the language, and the page is still
			// cacheable. That is the one reason the public face can be cached at
			// all once a shell is translated.
			if httpx.SurfaceOf(ctx) != httpx.SurfacePublic {
				out.CacheControl = "private, no-store"
			}
		}
		applyPrivacy(out, v.Sensitive)
		if v.RunsWASM {
			// Read from the view that was actually rendered: a handler that refused
			// answers with the kernel's fault page, which instantiates nothing, and a
			// redirect returned above this line — so neither carries the token, and no
			// branch here has to know that a refusal cleared the declaration.
			httpx.AllowWASM(ctx)
		}
		if v.Revalidate && !v.Sensitive && httpx.SurfaceOf(ctx) == httpx.SurfacePublic {
			// "Ask first" rather than "do not store": the copy may still be kept and
			// revalidated, and the one thing that cannot happen is a cache answering a
			// page it has not looked at since the owner published over it. The kernel
			// filled the one-minute default ahead of this; a page that knows its own
			// body moves replaces it.
			out.CacheControl = "no-cache"
		}
		return out, nil
	})
}

func applyPrivacy(out *httpx.Page, sensitive bool) {
	if sensitive {
		out.CacheControl = "no-store"
		out.ReferrerPolicy = "no-referrer"
	}
}

// Request is the caller of one request as every page through this shell sees
// them: the tenant, the principal, the path, and the language the shell answers
// in. Serve builds it for every page it mounts; a handler that renders a document
// of its own rather than a View — the gallery preview, which cannot arrive inside
// the shell's frame because a modal in an iframe owns that frame's focus — asks
// for the same value. It is exported because a second negotiation, written where
// the first one is not visible, is how one screen starts answering in two
// languages: the tenant's ceiling, the account preference and the order of the
// caller's own quality values are all decided here and nowhere else.
func (s Shell) Request(ctx context.Context) Request {
	r := read(ctx, s.Chrome)
	if s.Messages != nil {
		var preferred, accepted string
		if s.Locale != nil {
			preferred = s.Locale(ctx, r)
		}
		if req, ok := httpx.RequestFrom(ctx); ok {
			accepted = req.Header.Get("Accept-Language")
		}
		r.Locale = new(SelectLocale(s.Messages, TenantPreferences(r, preferred, accepted)...))
	}
	return r
}

// read is the one place a page learns about its caller.
func read(ctx context.Context, c Chrome) Request {
	var r Request
	if t, ok := tenancy.FromContext(ctx); ok {
		r.Tenant = t
	}
	if p, ok := tenancy.PrincipalFrom(ctx); ok && p.UserID != uuid.Nil {
		r.Principal, r.SignedIn = p, true
	}
	if req, ok := httpx.RequestFrom(ctx); ok {
		r.Path = req.URL.Path
	}
	if c.Theme == "" {
		r.Inline = []g.Node{InlineScript(ctx, beforePaint)}
	}
	return r
}

// backLabel is the shell's way out, in the language this request is answered in.
//
// A label is copy, and copy is the catalogue's: the shell's own words are the
// fallback a deployment that ships no copy for the key is shown, which is what a
// shell with no catalogue, no key, or no language for this request keeps printing.
func (s Shell) backLabel(loc *Locale) string {
	if s.BackKey == "" {
		// The shell named no catalogue entry for its way out. This package still
		// words the destination its own label names, and carries the translation of
		// that one destination under fault.back — so the label is asked of the
		// reader's language here rather than printed as the composition wrote it.
		// A shell whose words name somewhere else keeps them untranslated: see backWay.
		return backWay(loc, s.BackLabel)
	}
	if loc == nil {
		return s.BackLabel
	}
	return loc.Text(s.BackKey, s.BackLabel)
}

// WayBack is the address and the words of this shell's way out, in the language
// this request is answered in. A renderer outside this package that draws a page
// of its own asks this rather than reading BackLabel itself, because a link a
// person follows from a refusal is the sentence the refusal is worth having.
func (s Shell) WayBack(loc *Locale) (href, label string) {
	return s.Back, s.backLabel(loc)
}

// regionRequested says the fetch in hand is a region asking for its own contents:
// htmx puts HX-Request on every fetch it makes, and two of those fetches ask for a
// page rather than a piece of one.
//
// A boosted link (hx-boost) is the browser navigating with the address in the bar,
// and a history restore is the controller replaying a GET for a document the person
// is going back to: both expect a document, and answering either with a fragment
// would leave a person looking at a bare list where a page used to be. The region's
// own refresh is neither — it is the plain fetch htmx makes when the controller
// triggers it, and it is the only fetch of this address that wants the swap answer.
//
// The header is a client's claim, read for one decision and one only — whether to
// wrap a body in a document — and never for who may see it, which the route's own
// guard decided before this line.
func regionRequested(ctx context.Context) bool {
	req, ok := httpx.RequestFrom(ctx)
	if !ok || req.Header.Get("HX-Request") != "true" {
		return false
	}
	return req.Header.Get("HX-Boosted") != "true" &&
		req.Header.Get("HX-History-Restore-Request") != "true"
}

// streamConnect is the address a live region opens its connection at: the
// kernel's live door, with this page's topics named once in its query.
func streamConnect(topics []string) string {
	return httpx.StreamAddress + "?" + url.Values{"topics": {strings.Join(topics, ",")}}.Encode()
}

// regionAddress is the address a refresh re-reads: the one the region was drawn
// at, paging included, so a person on the second page of a list is not returned
// to the first by somebody else's notice. The composed value has to pass the
// kernel's own local-path rule to be used: the path and the query are the
// caller's bytes, and a refresh must not be pointed at another site.
func regionAddress(ctx context.Context, r Request) string {
	req, ok := httpx.RequestFrom(ctx)
	if !ok || req.URL.RawQuery == "" {
		return r.Path
	}
	if paged := r.Path + "?" + req.URL.RawQuery; httpx.LocalPath(paged) {
		return paged
	}
	return r.Path
}

// refusal is an error's status, detail and — when the writer of the refusal named
// its own sentence, which is what carries the catalogue key a page asks that
// sentence under — that sentence itself: a problem's own, or a 500.
func refusal(err error) (int, string, *kitfault.Reason) {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Status, p.Detail, p.Refusal
	}
	return http.StatusInternalServerError, "", nil
}
