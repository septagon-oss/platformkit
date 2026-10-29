package page

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/screen"
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
	// View binds this route to a declared screen; Response defaults to input.
	View     screen.ViewRef
	Response string
	// Exception classifies redirect, protocol, export or preview responses.
	Exception, Reason string
	// Targets checks composed form addresses against their operation identities.
	Targets []screen.Target
	// MintsCredential prevents storing this page's credential-bearing response.
	MintsCredential           bool
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
	// Subject says what this page is about, so that the refusal it may answer is
	// about the same thing: a person sent away from one record is told the record is
	// gone and offered its list, not told the address is missing. A hand-written page
	// leaves it empty and is refused about its address, which is what a page that names
	// no subject can truthfully say.
	Subject Subject
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
// the frame, turns a SeeOther into the redirect and a refusal into the same
// refusal page a kernel guard renders — worded through the locale this shell
// negotiated — whatever the verdict.
//
// A read that broke is drawn as that page too, at its own 5xx, for the person who
// opened the address: what they can act on is "we couldn't load the tasks, try again
// here", and no page in this repository has ever been improved by their seeing
// "Internal Server Error". The reason behind the failure goes to the log, which is where
// a cause belongs, and the request keeps its 500, so a monitor watching statuses still
// sees the failure the page refuses to name.
func Serve[I any](r *httpx.Router, s Shell, rt Route, auth httpx.Auth, handler Handler[I]) {
	if s.Messages != nil {
		_ = SelectLocale(s.Messages) // validate the provider at composition
	}
	op := screenOperation(r, s, rt)
	httpx.HTML(r, op, auth, func(ctx context.Context, in *I) (*httpx.Page, error) {
		r := readPageRequest(ctx, s)
		v, err := handler(ctx, r, in)
		if err != nil {
			if to, ok := errors.AsType[httpx.SeeOther](err); ok {
				out := httpx.Redirect(ctx, string(to))
				applyPrivacy(out, v.Sensitive)
				return out, nil
			}
			why := refusal(err)
			if why.Status >= http.StatusInternalServerError {
				// The page says what could not be loaded and offers the address again; the
				// reason stays here, with the caller who can fix it.
				slog.ErrorContext(ctx, "page: the read behind this page failed",
					"path", r.Path, "status", why.Status, "error", err)
			}
			asked, _ := httpx.RequestFrom(ctx)
			if httpx.WantsValue(asked) {
				// The client named a JSON media type and refused markup, so the refusal of a
				// page route is the same problem document every other refusal of this address
				// would have been, written by the kernel's one encoder — the rule
				// kit/httpx/fault.go states for a guard's refusal, asked of the same file
				// rather than copied. A client that named nothing has no opinion to honour and
				// gets the page this route exists to give.
				return nil, err
			}
			// Handler refusals use the same localized fault view as guard failures.
			// Preserve sensitivity from the refused view. The reference is the request's own
			// id, which is why a handler's refusal is quotable at all: the id middleware ran
			// before this handler was reached, and the problem body the same refusal takes to
			// a program carries it as its instance (kit/httpx's stampRequestID).
			refused := fault(verdict{status: why.Status, detail: shown(why), key: why.Key,
				reference: httpx.RequestID(ctx), loc: r.Locale, sub: rt.Subject,
				back: s.Back, backLabel: s.BackLabel, path: r.Path,
				ask: asked, signIn: s.Chrome.SignIn})
			refused.Sensitive = v.Sensitive
			v = refused
		}
		attachScreenView(rt, err, &v)
		if r.Locale != nil {
			if v.Language == "" {
				v.Language = r.Locale.Language
			} else if v.Language != r.Locale.Language {
				r.Locale = new(SelectLocale(s.Messages, v.Language))
			}
		}
		out, err := renderPageView(ctx, s, r, v)
		if err != nil {
			return nil, err
		}
		applyPageResponsePolicy(ctx, s, v, out)
		return out, nil
	})
}

func pageOperation(s Shell, rt Route) huma.Operation {
	op := huma.Operation{
		OperationID: rt.ID, Method: rt.Method, Path: rt.Path, Summary: rt.Summary,
		Tags: []string{s.Tag}, Errors: rt.Errors,
	}
	if rt.StreamsBody {
		key, streamed := httpx.StreamedBody()
		op.Extensions = map[string]any{key: streamed}
	}
	if rt.MintsCredential {
		if op.Extensions == nil {
			op.Extensions = map[string]any{}
		}
		op.Extensions[httpx.CredentialExtension] = true
	}
	if s.Chrome.SignIn != "" {
		httpx.SignIn(&op, s.Chrome.SignIn)
	}
	return op
}

func readPageRequest(ctx context.Context, s Shell) Request {
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

func renderPageView(ctx context.Context, s Shell, r Request, v View) (*httpx.Page, error) {
	var err error
	status := v.Status
	if status == 0 {
		status = http.StatusOK
	}
	// Region refreshes return only their contents, including refusals;
	// the guarded read and route remain the same as a full document.
	var out *httpx.Page
	if regionRequested(ctx) && (len(v.Live) > 0 || status >= http.StatusBadRequest) {
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
	return out, err
}

func applyPageResponsePolicy(ctx context.Context, s Shell, v View, out *httpx.Page) {
	if s.Messages != nil {
		out.ContentLanguage, out.Vary = v.Language, "Accept-Language"
		// Account preferences are private. Public language negotiation depends
		// only on the request, so Vary is sufficient there.
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
}
func applyPrivacy(out *httpx.Page, sensitive bool) {
	if sensitive {
		out.CacheControl = "no-store"
		out.ReferrerPolicy = "no-referrer"
	}
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
		r.Path, r.Host = req.URL.Path, req.Host
	}
	if c.Theme == "" {
		r.Inline = []g.Node{InlineScript(ctx, beforePaint)}
	}
	return r
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

// refusal is a handler's error read as the verdict it is: its own problem, or the 500
// standing in for anything else. The verdict is handed over whole rather than flattened to
// a status and a sentence, because the two fields a person's page needs are precisely the
// two the problem document does not carry (problem.Problem's Key and Diagnostic), and both
// doors read them through shown and fault below. So the two shapes of one refusal cannot
// disagree about what a browser may be shown.
func refusal(err error) *problem.Problem {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p
	}
	return problem.New(http.StatusInternalServerError, "")
}
