package main

// What a person sees when the application refused them before any screen was reached.
//
// The kernel refuses requests for reasons no page handler ever learns about: the write
// came from another site, the tenant behind the Host header serves nothing, a handler
// panicked. Those refusals arrive as an RFC 9457 problem document, which is exactly right
// for a client reading a value and useless for a person who navigated to a URL and is now
// looking at JSON in a browser window with a request id they cannot select cleanly.
//
// The application, not kit/app, decides this: a failure page is chrome — the brand in the
// title tag, the stylesheet that makes it look like the application rather than like a
// crash, and where the one link goes — and chrome is ui, which kit may not import. So the
// look of a refusal belongs to whoever owns the look of everything else.
//
// This page has no sidebar and no account menu, and that is deliberate rather than
// unfinished: the navigation a signed-in person gets is built from what *they* may reach,
// and a refusal page is most often served to somebody who is not signed in, or whose
// session is exactly what failed. A page that needs a caller to render is no good at the
// moment somebody has no caller to be.

import (
	"context"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/page"
)

// faultPage renders the refusal for this application's shell.
//
// Assets names the admin module's own asset route, so the failure page is styled by the
// same sheet as the pages beside it. The fingerprint on the link is this composition's,
// not the admin shell's: it differs whenever the default theme changes rather than when
// the shell's extra layers do, which costs a redundant stylesheet fetch and nothing else —
// the file behind the path is the same one, and the classes a refusal page emits (a
// toolbar, an alert, a link) are all in the shell's own list.
func faultPage() httpx.Fault {
	return page.FaultHandler(page.Shell{
		Chrome: page.Chrome{
			Brand:      "PlatformKit",
			Assets:     "/admin/assets",
			Stylesheet: ui.Compose(design.Default()),
			SignIn:     "/admin/login",
		},
		Frame:     func(_ context.Context, _ page.Request, body []g.Node) g.Node { return g.Group(body) },
		Back:      "/admin",
		BackLabel: "Back to the workspace",
	})
}

// appOptions is the composition every entry point of this binary shares. It exists so
// that the line wiring the failure page is one line rather than two — an entry point
// that spells its own options out drifts, and the drift is invisible until somebody is
// refused in production and sees JSON, while the other entry point shows a page.
//
// It is also the reason a test can claim the application does this: the test composes
// through here, so it exercises the wiring rather than a copy of it.
func appOptions(c composition, role app.Role) app.Options {
	return app.Options{
		Tenants:      c.tenants,
		Authorize:    c.auth,
		Entitle:      c.plans,
		Authenticate: c.auth.Authenticate,
		Fault:        faultPage(),
		Role:         role,
		Transports:   transports(),
	}
}
