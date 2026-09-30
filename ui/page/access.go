package page

// access.go is the browser's half of asking for access: the form the refusal
// page posts to, and the page that says the ask was sent.
//
// Two routes, one command. The JSON door beside it (kit/app/access.go) calls the
// same kit/httpx command, so neither is a second implementation of the ask
// (docs/adr/0007) — and the mount is here, in the layer that draws the refusal,
// because the ask is the refusal's sequel and only the refusal page links it.
//
// The confirmation page reads nothing back and says only what the POST that
// redirected to it just did. It cannot say "your request from 14:02", because
// that would be a read of the audit trail, which needs the audit module's own
// permission — the one a person who had it would not have needed to ask for. A
// bookmarked confirmation page therefore says something that may no longer be
// true of the world, and that is the honest limit of a page with no row behind
// it rather than a thing to fix by showing a request parameter back at the
// reader.

import (
	"context"
	"net/http"
	"net/url"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/ui/components"
)

// askSent is where the form lands, and askPath and askSentPath are the two
// addresses relative to the workspace: the composition mounts them with
// MountAccess and names the first in its Shell.Ask, so the refusal page's form
// and this file's routes cannot drift apart silently.
const (
	askPath     = "/access-request"
	askSentPath = "/access-request/sent"
)

// MountAccess mounts the two page routes on the workspace router. A composition
// that wires no AskForAccess never calls it, and Shell.Ask stays empty: the
// refusal page then draws no ask control at all.
func MountAccess(r *httpx.Router, s Shell) {
	Serve(r, s, Route{ID: "app-access-request", Method: http.MethodPost, Path: askPath,
		Summary: "Ask for the access this page refused",
		Errors:  []int{http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}, httpx.SignedIn(), func(ctx context.Context, req Request, in *askForm) (View, error) {
		form, err := url.ParseQuery(string(in.RawBody))
		if err != nil {
			return View{}, problem.New(http.StatusUnprocessableEntity, "this form could not be read")
		}
		// The refused request's language travels with the ask, so the notice a
		// manager reads can be worded in the words the person who asked was
		// reading. Whether to prefer the recipient's own instead is the
		// composition's, and it owns the wording.
		if err := httpx.Ask(httpx.WithRefusalLanguage(ctx, spoken(req)), httpx.AccessAsk{
			Permission: form.Get("permission"), Path: form.Get("path"),
		}); err != nil {
			return View{}, err
		}
		return View{}, httpx.SeeOther(httpx.AppRoot + askSentPath)
	})

	Serve(r, s, Route{ID: "app-access-request-sent", Method: http.MethodGet, Path: askSentPath,
		Summary: "Confirmation that an access request was sent",
	}, httpx.SignedIn(), func(_ context.Context, req Request, _ *Empty) (View, error) {
		loc := req.Locale
		title := word0(loc, "fault.sent", "Your request was sent")
		body := word0(loc, "fault.sent_body",
			"The people in this tenant who can grant access have been told what you need.")
		v := View{Title: title, Status: http.StatusOK, Body: []g.Node{
			components.Toolbar(components.ToolbarProps{Title: title}),
			components.Alert(components.AlertProps{Tone: "success", Message: body, Bordered: true}),
			components.Link(components.LinkProps{Label: s.BackLabel, Href: s.Back}),
		}}
		if loc != nil {
			v.Language = loc.Language
		}
		return v, nil
	})
}

// language is the tag this page was drawn in, or "" for a shell with no
// catalogue at all.
func spoken(r Request) string {
	if r.Locale == nil {
		return ""
	}
	return r.Locale.Language
}

// askForm is the submitted refusal-page form: the grant and the address, both
// written into hidden fields by the page that refused. Nothing else is
// submittable, because nothing else an ask may decide.
type askForm struct {
	RawBody []byte `contentType:"application/x-www-form-urlencoded"`
}
