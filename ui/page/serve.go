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
}

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
// the frame, turns a SeeOther into the redirect and a 4xx problem into Fault,
// and lets a 5xx keep the kernel's problem document and log line.
func Serve[I any](api *httpx.API, s Shell, rt Route, auth httpx.Auth, handler Handler[I]) {
	op := huma.Operation{
		OperationID: rt.ID, Method: rt.Method, Path: rt.Path, Summary: rt.Summary,
		Tags: []string{s.Tag}, Errors: rt.Errors,
	}
	if s.Chrome.SignIn != "" {
		httpx.SignIn(&op, s.Chrome.SignIn)
	}
	httpx.HTML(api, op, auth, func(ctx context.Context, in *I) (*httpx.Page, error) {
		r := read(ctx, s.Chrome)
		v, err := handler(ctx, r, in)
		if err != nil {
			var to httpx.SeeOther
			if errors.As(err, &to) {
				return nil, err
			}
			status, detail := refusal(err)
			if status >= http.StatusInternalServerError {
				return nil, err
			}
			v = Fault(status, detail, s.Back, s.BackLabel)
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
		return httpx.Document(Document(s.Chrome, r, v, body), status)
	})
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
		r.Inline = []g.Node{httpx.Script(ctx, beforePaint)}
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
