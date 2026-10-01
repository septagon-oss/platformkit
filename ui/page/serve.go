package page

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
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
	ID, Method, Path, Summary string
	Errors                    []int
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
	if s.Chrome.SignIn != "" {
		httpx.SignIn(&op, s.Chrome.SignIn)
	}
	httpx.HTML(r, op, auth, func(ctx context.Context, in *I) (*httpx.Page, error) {
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
		v, err := handler(ctx, r, in)
		if err != nil {
			if to, ok := errors.AsType[httpx.SeeOther](err); ok {
				out := httpx.Redirect(ctx, string(to))
				applyPrivacy(out, v.Sensitive)
				return out, nil
			}
			status, detail := refusal(err)
			if status >= http.StatusInternalServerError {
				return nil, err
			}
			// The refusal of a handler is the same page the kernel's guards render
			// (fault.go) and is worded the same way: through the locale this shell just
			// negotiated, with the sentence this shell ships for that verdict. A person
			// who was refused by a module is looking at the page all the same, and the
			// copy they are shown is not the reason the language was bought for the shell
			// that mounted them. `fault` claims a language only over a sentence it
			// actually replaced, so a verdict no catalogue speaks stays English and says
			// so — and the reference is empty here because a handler's own 4xx carries no
			// request id of its own.
			refused := fault(status, detail, r.Locale, "", s.Back, s.BackLabel)
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
		var body g.Node
		if v.Bare {
			body = Bare(v.Body)
		} else {
			body = s.Frame(ctx, r, v.Body)
		}
		out, err := Render(Document(s.Chrome, r, v, body), status)
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

// refusal is an error's status and detail: a problem's own, or a 500.
func refusal(err error) (int, string) {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Status, p.Detail
	}
	return http.StatusInternalServerError, ""
}
