package main

// What a person sees when the application refused them before any screen was reached.
//
// The kernel refuses requests for reasons no page handler learns about: the write
// came from another site, the tenant behind the Host header serves nothing, a handler
// panicked. Those arrive as an RFC 9457 problem document — right for a client reading
// a value, useless for a person looking at JSON in a browser window.
//
// The application, not kit/app, decides this: a failure page is chrome — the brand in
// the title tag, the sheet that makes it look like the application rather than a
// crash, where the one link goes — and chrome is ui, which kit may not import.
//
// The page has no sidebar and no account menu, deliberately: the navigation a
// signed-in person gets is built from what they may reach, and a refusal is most
// often served to somebody with no caller to be.

import (
	"context"
	"errors"
	"slices"
	"sync"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// faultPage renders the refusal for this application's shell.
//
// Assets names the admin module's own asset route, so the failure page is styled by
// the same sheet as the pages beside it. The fingerprint on the link is this
// composition's, not the admin shell's: it changes when the default theme changes
// rather than when the shell's extra layers do, which costs a redundant stylesheet
// fetch and nothing else — the file behind the path is the same one, and the classes
// a refusal page emits are all in the shell's own list.

// The addresses this product pins, spelled the way the kernel composes them. A module
// may not write a prefix; the product may, and here it must: the failure page is
// rendered by the kernel's guard before any module is consulted — a refused visitor
// has no session to resolve and no module to ask — so the chrome it needs is knowledge
// the composition carries: where the stylesheet is, where signing in is, and what the
// workspace is called.
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
		// The installation's own word, the same constant the workspace's face
		// falls back to: this file composes pages of this application and has no
		// business spelling its own name twice.
		Brand:      brandName,
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

func faultPage(messages page.Messages, granter page.Granter) httpx.Fault {
	return page.FaultHandler(page.Shell{
		Chrome:    faultChrome(),
		Frame:     faultFrame,
		Back:      pinnedHome,
		BackLabel: "Back to the workspace",
		// Who may hand out what the page refused, as a role and never as a name:
		// the grant that gates role management is the auth module's fact, and its
		// label is that module's words, read off the manifest that defines it.
		Granter: granter,
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
func workspaceCatalog(surface *keptSurface) func(api *httpx.API) {
	// The gate is built here, one per composition, and not at file scope: see
	// referenceGate. It carries the capability question because this is the one
	// place both halves meet: the document is where a savable declaration becomes a
	// promise, and only this composition knows whether it kept anything to honour it.
	return app.WorkspaceCatalogRoute(documentBuilder(&documentGate{surface: surface}))
}

// documentBuilder is the body of that mount: the same catalogues read once, the
// same gate, and the resources the request's caller may reach. It is a function
// of its gate because the one thing about this handler that is not a translation
// is a refusal, and a refusal nobody can call is a refusal nobody tests.
func documentBuilder(gate *documentGate) func(context.Context, []httpx.Resource) (*screens.Catalog, error) {
	// The catalogues this composition answers in. The document is read by a shell
	// that is not a browser, and every word in it is one a person will read, so it
	// is negotiated the same way the page beside it is — the request's own
	// Accept-Language, restricted to the languages the tenant it resolved to is
	// served in, that tenant's default behind them. A handler that negotiated less
	// would answer a pt-only tenant in English from this address while the screen
	// one click away spoke Portuguese.
	messages := catalogues()
	return func(ctx context.Context, resources []httpx.Resource) (*screens.Catalog, error) {
		// A reading hint that names a module this installation did not compose is
		// a composition mistake rather than a module's, and the document is the
		// thing the promise is made in, so the check is made once, here, against
		// the list this process actually registered. A Spec's own mount check
		// cannot see a registration made after it, and a mount-time check cannot
		// see one made after the mount: a worker process registers only the
		// resources its jobs read, so the list standing at boot is not the list a
		// shell would be served — which is why this rides the first build of the
		// document rather than the mount. What the answer costs is the document
		// and nothing else: every entry in it is a reference a shell would follow,
		// so the composition that cannot answer the question refuses to publish an
		// answer rather than publishing a half of one. Boot stays up: every route
		// this process mounted resolves its own references at its own mount, and
		// the document is the only thing this refusal can name.
		if err := gate.check(resources); err != nil {
			return nil, err
		}
		var preferences []string
		if r, ok := httpx.RequestFrom(ctx); ok {
			preferences = append(preferences, r.Header.Get("Accept-Language"))
		}
		if tenant, ok := tenancy.FromContext(ctx); ok {
			preferences = page.TenantPreferences(page.Request{Tenant: tenant}, preferences...)
		}
		selected := page.SelectLocale(messages, preferences...)
		// One formatter, no arguments: no hint in this contract takes a verb, so the
		// document asks for a key and its fallback and nothing else.
		var text screens.Text = func(key, fallback string) string {
			return selected.Formatter.Text(key, fallback)
		}
		document := screens.DescribeInstalled(ctx, resources, text, selected.Language, gate.installed())
		// Where a record's picture lives, said once for the whole document rather
		// than once per field that holds a file id. The address is the file
		// module's — it mounts the door — and the document is the kernel's, so this
		// composition is the only party that has met the two and the only one that
		// may write the string. A shell derives nothing.
		document.Pictures = &screens.Pictures{
			Source: filecontracts.PictureSource,
			Widths: slices.Clone(filecontracts.VariantWidthSet),
		}
		return &document, nil
	}
}

// documentGate is the composition's once-per-mount question about the document it
// is about to publish, which is every question a reading declaration asks about the
// modules this process actually registered: does each `reference` name a resource
// somebody composed, and is each `primaryCommand` a verb this resource mounts?
//
// The once is there because the answer is about the composition, which does not
// change after boot, and asking it per request would be a walk of every schema on
// somebody's page load. It is held by one mount rather than by the process because a
// process composes several applications — every test binary in this repository boots
// the reference composition more than once — and a process-wide once lets the first
// build of a document decide for every later one: a second composition's dangling
// declaration would go unchecked, and a correct composition behind a broken one would
// serve nothing but 500s. The question each composition asks is about the list *it*
// registered.
type documentGate struct {
	once  sync.Once
	fault error
	// surface is what this composition answered about its own optional capabilities
	// at the moment the document was built; nil is a gate built without a
	// composition standing behind it, which answers "nothing installed".
	surface *keptSurface
}

// installed is the capability answer the document is written with.
func (g *documentGate) installed() screens.Installed {
	return g.surface.installed()
}

// check asks the questions the first time it is called and remembers the answer.
// sync.Once is the synchronisation: a reader here is a goroutine that has just
// returned from Do, which happens after the answer was written.
func (g *documentGate) check(resources []httpx.Resource) error {
	g.once.Do(func() {
		for _, question := range []func([]httpx.Resource) string{rest.CheckReferences, rest.CheckRowHints} {
			if bad := question(resources); bad != "" {
				g.fault = errors.New("apps/platformkit: " + bad)
				return
			}
		}
	})
	return g.fault
}

// workspaceFace mounts every module-less route this product owns on the workspace
// surface. app.Options.WorkspaceCatalog is the only seam that hands a composition
// the *httpx.API of /api/v1/app/… — no module may compose that prefix, and kit may
// not import ui — and it takes an arbitrary mount function, so a second such route
// needs no new kernel seam, only this line. The field's name is now narrower than
// what runs through it: the catalogue and the connection document are both mounted
// here, and both are the composition's because both addresses are the kernel's and
// both bodies are ui's.
func workspaceFace(routes ...func(api *httpx.API)) func(api *httpx.API) {
	return func(api *httpx.API) {
		for _, mount := range routes {
			mount(api)
		}
	}
}

// faultShell is the chrome the two ask pages are drawn with: the same frame, the
// same catalogue and the same way on as the refusal page they follow, built once
// per composition. One page per shell, and the shell is this file's to build —
// the admin module builds its own, for the screens it mounts.
func faultShell(messages page.Messages, granter page.Granter) page.Shell {
	return page.Shell{
		Chrome:    faultChrome(),
		Frame:     faultFrame,
		Back:      pinnedHome,
		BackLabel: "Back to the workspace",
		Granter:   granter,
		Ask:       pinnedAsk,
		Messages:  messages,
	}
}
