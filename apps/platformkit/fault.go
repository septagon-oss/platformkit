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

	"github.com/septagon-oss/platformkit/pkit"
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
	// pinnedRegisterAPI is the registration door as the public surface composes
	// it: /api/v1/public/<module>/… The shell's register form posts there, and
	// TestPinnedAddresses asks the running server that it answers. The login
	// door above keeps the workspace address because that is where it always
	// was; this door is anonymous and public, so it lives where an anonymous
	// caller is answered.
	pinnedRegisterAPI = "/api/v1/public/auth/register"
	// pinnedPublicFile is the file module's public door: a visitor who may see a
	// file asks it there, and only there, because the public surface is the one
	// that answers an anonymous caller and refuses to set a cookie while doing
	// it. The site that links a logo is composed by web.Module, which does not
	// know the file module exists, so the address is the product's to write —
	// and the case named at web.Deps.PublicFileURL in modules.go asks a real
	// uploaded file whether the address answers, on the running server.
	pinnedPublicFile = "/api/v1/public/file/files"
	// pinnedHome is the way on a refusal offers: the workspace root, which the
	// admin shell claims as its own home and serves behind httpx.SignedIn() — a
	// guard that asks no permission, so it cannot refuse the person this page is
	// for.
	//
	// The address was "/app/dashboard", which is where the same shell puts its
	// dashboard when a product's home claimed the root first. This composition
	// claims the root, so that fallback answered 404 and the one link on a refusal
	// page led a person out of the application — the dead end the walkthroughs met,
	// drawn in different ink. TestPinnedAddresses asks the running server that every
	// pin here answers; the journey case follows the link the page itself emits.
	pinnedHome = pinnedWorkspace
	// pinnedUsers is the generated person screen's address, which is what an
	// access notice links to: the page where a person's roles are ticked.
	pinnedUsers = "/app/user/users"
	// pinnedAsk is the browser's ask door, mounted by page.MountAccess below.
	pinnedAsk = "/app/access-request"
)

// faultChrome is the look every page this file composes shares: the refusal and
// the two pages of an ask are the same application's chrome, drawn by the same
// four lines.
func faultChrome() page.Chrome {
	return page.Chrome{
		Brand:      "PlatformKit",
		Assets:     pinnedAssets,
		Stylesheet: ui.Compose(design.Default()),
		SignIn:     pinnedSignIn,
	}
}

// faultFrame is the column these pages are read in: the foundation's frame with
// no navigation, the same narrow centred column the sign-in page already uses.
//
// It used to be `g.Group(body)` — no container at all — because this chrome has
// no sidebar and no account menu to draw (see this file's header). A frame is
// not only the navigation, though: it is what bounds the text. With no container
// every sentence of a refusal, and of the confirmation page that follows an ask,
// ran the whole viewport — 194 characters on a line at 1440px, which is over
// twice the 75-character measure the design floor refuses. The pages a shell
// with a frame renders were never measured by that floor either, because the
// repository's eye (`e2e/design-audit.spec.ts`) looks inside `main`, which a
// frameless page has no way to have; the fault pages are outside it either way.
func faultFrame(_ context.Context, _ page.Request, body []g.Node) g.Node {
	return page.Bare(body)
}

func faultPage(c composition) httpx.Fault {
	messages := c.messages
	return page.FaultHandler(page.Shell{
		Chrome:    faultChrome(),
		Frame:     faultFrame,
		Back:      pinnedHome,
		BackLabel: "Back to the workspace",
		// Who may hand out what the page refused, as a role and never as a name:
		// the grant that gates role management is the auth module's fact, and its
		// label is that module's words, read off the manifest that defines it.
		Granter: c.granter,
		// The ask door exists in this composition, so the refusal page may offer
		// it. A product that wired no AskForAccess leaves this empty and the page
		// draws no button — the kernel never offers a door it has not mounted.
		Ask: pinnedAsk,
		// The catalogues this application already composed. Without them the shell
		// is what ui/page/fault.go documents as "a shell that ships no catalog":
		// refusalLocale negotiates from nothing, the guard's English line is shown,
		// and the refusal sentences this repository ships are copy nothing reads at
		// the application that shipped them. The line is what the page is rendered
		// *with* — a person refused by a guard ahead of routing is refused by this
		// shell, not by the ones page.Serve mounts — and TestPinnedAddresses and the
		// refusal cases here are asked of the page this literal builds.
		Messages: messages,
	})
}

// workspaceCatalog is the mount for the document a native shell reads to know what
// this application has. The kernel owns the address and ui owns the body, and this
// is the one line that joins them. It is written once on purpose: every entry point
// and the test harness mount this same mount, so the document a gate compares
// against is the one this file wires — the contract gate cannot pass on a copy of
// the renderer while the shipped binary publishes another. The renderer's type is
// carried on the call, so the OpenAPI document names the fields a shell parses
// rather than an empty object — see app.WorkspaceCatalogRoute.
func workspaceCatalog() func(api *httpx.API) {
	return app.WorkspaceCatalogRoute(func(ctx context.Context, resources []httpx.Resource) (*screens.Catalog, error) {
		document := screens.Describe(ctx, resources)
		return &document, nil
	})
}

// appOptions is the same composition app.go sentences, read as app.Options: the
// in-process answer for a test that wants the handler rather than a process, and
// the reason the two cannot disagree about who provides what. The four fields
// that belong to a process rather than to a composition — the role, the
// transports, the stores, the installation's host — are filled here, because pkit
// leaves them to whoever is starting something.
func appOptions(cfg config.Config, c composition, role app.Role) app.Options {
	p, err := sentences(cfg, c).Plan(pkit.Deployment{Environment: pkit.Development, Config: cfg})
	if err != nil {
		panic("platformkit: " + err.Error())
	}
	opts := p.Options()
	opts.Role = role
	// The four fields pkit leaves to whoever is starting something. Transports and
	// Caches are not decoration: kit/app refuses a role whose mode has no
	// constructor and a composition whose cache.adapter names a store it never
	// learned to reach, so a test that lost these lines fails in app.New with a
	// message about memory and jetstream, or about Caches.Valkey, rather than
	// booting the wrong thing.
	opts.Transports = transports()
	opts.Caches = caches()
	opts.Installation = app.Installation{Host: cfg.Server.InstallationHost}
	return opts
}

// faultShell is the chrome the two ask pages are drawn with: the same frame, the
// same catalogue and the same way on as the refusal page they follow, built once
// per composition. One page per shell, and the shell is this file's to build —
// the admin module builds its own, for the screens it mounts.
func faultShell(c composition) page.Shell {
	return page.Shell{
		Chrome:    faultChrome(),
		Frame:     faultFrame,
		Back:      pinnedHome,
		BackLabel: "Back to the workspace",
		Granter:   c.granter,
		Ask:       pinnedAsk,
		Messages:  c.messages,
	}
}
