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
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// faultPage renders the refusal for this application's shell.
//
// Assets names the admin module's own asset route, so the failure page is styled by the
// same sheet as the pages beside it. The fingerprint on the link is this composition's,
// not the admin shell's: it differs whenever the default theme changes rather than when
// the shell's extra layers do, which costs a redundant stylesheet fetch and nothing else —
// the file behind the path is the same one, and the classes a refusal page emits (a
// toolbar, an alert, a link) are all in the shell's own list.
// The addresses this product pins, spelled the way the kernel composes them.
//
// A module may not write a prefix; the product may, and here it must. The
// failure page is rendered by the kernel's own guard before any module is
// consulted — a refused visitor has no session to resolve and no module to ask
// — so the chrome it needs is knowledge the composition carries: where the
// shell's stylesheet is, where signing in is, and what the workspace is called.
// TestPinnedAddresses asks the running server that every one of them answers,
// which is what makes pinning them safe rather than hopeful.
const (
	pinnedWorkspace = "/app"
	pinnedSignIn    = "/app/admin/login"
	pinnedAssets    = "/app/admin/assets"
	// pinnedSignInAPI is the auth module's own door on the workspace surface,
	// which the admin shell's form posts to. The module declares the route
	// relative to itself (/login on its App router); the composition is the one
	// place that knows both the surface prefix and that this installation
	// composes auth, so it is the one place allowed to write the whole address —
	// and TestPinnedAddresses asks the running server that it answers.
	//
	// The address is unchanged by this release, and that is the point: a
	// workspace's JSON keeps the address it always had (/api/v1/<module>/…),
	// because moving it would break every client already installed for no gain.
	// What moved is where its documents are (/app/<module>/…), and the public
	// doors, which took their own prefix.
	pinnedSignInAPI = "/api/v1/auth/login"
	// pinnedPublicFile is the file module's public door: a visitor who may see a
	// file asks it there, and only there, because the public surface is the one
	// that answers an anonymous caller and refuses to set a cookie while doing
	// it. The site that links a logo is composed by web.Module, which does not
	// know the file module exists, so the address is the product's to write —
	// and the case named at web.Deps.PublicFileURL in modules.go asks a real
	// uploaded file whether the address answers, on the running server.
	pinnedPublicFile = "/api/v1/public/file/files"
)

func faultPage() httpx.Fault {
	return page.FaultHandler(page.Shell{
		Chrome: page.Chrome{
			Brand:      "PlatformKit",
			Assets:     pinnedAssets,
			Stylesheet: ui.Compose(design.Default()),
			SignIn:     pinnedSignIn,
		},
		Frame:     func(_ context.Context, _ page.Request, body []g.Node) g.Node { return g.Group(body) },
		Back:      pinnedWorkspace,
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
func appOptions(cfg config.Config, c composition, role app.Role) app.Options {
	return app.Options{
		// The installation's own host, from the configuration. It is the only
		// address that serves the control plane, and the console of the
		// installation itself — /ops — answers there and nowhere else.
		Installation: app.Installation{Host: cfg.Server.InstallationHost},
		// The document a native shell reads to know what this application has.
		// The kernel owns the address and ui owns the body, and this is the one
		// line that joins them.
		WorkspaceCatalog: func(ctx context.Context, resources []httpx.Resource) (any, error) {
			return screens.Describe(ctx, resources), nil
		},
		Tenants:      c.tenants,
		Authorize:    c.auth,
		Entitle:      c.plans,
		Authenticate: c.auth.Authenticate,
		Fault:        faultPage(),
		Role:         role,
		Transports:   transports(),
	}
}
