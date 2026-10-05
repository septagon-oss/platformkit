// Package internal is the shell's implementation: one chrome, one frame, the
// six pages written by hand, the catalog, and the screens ui/screens generates
// for every resource kit/rest registered before this module was composed.
package internal

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sort"
	"strings"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/export"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// Registration is the way in for somebody the tenant has no account for yet:
// which form the shell renders and the door that form posts to. Only the
// composition knows it wired a registration service, so only the composition
// names this — the same reason it names SignIn rather than writing the auth
// module's address down here.
type Registration struct {
	// Kind is which of the two forms the page renders.
	Kind RegistrationKind
	// Address is the composed door the form posts to.
	Address string
}

// RegistrationKind is which registration form the shell renders. The two are
// what ui/assets/js/session.js already distinguishes by name.
type RegistrationKind int

const (
	// RegistrationKindEmail asks for an address alone: the tenant then invites
	// the person, who chooses a password from the mail.
	RegistrationKindEmail RegistrationKind = iota + 1
	// RegistrationKindPassword takes a password and its confirmation, and the
	// account waits for the mailbox link before it can sign in.
	RegistrationKindPassword
)

// form is the name the session controller selects this form by.
func (k RegistrationKind) form() string {
	if k == RegistrationKindPassword {
		return "register-password"
	}
	return "register"
}

// brand is what the shell calls itself when the tenant has no name.
const brand = "PlatformKit"

// chromeTextSize is the one body step the frame's own text takes: the tenant name, the caller and the
// build stamp are the chrome, and the design floor allows a page two body sizes in total — so three
// sizes in the chrome alone leaves the page's own copy nothing. `sm` is what two of the three already
// took, and it is the sidebar's own nav step, which leaves `base` free for the page inside <main>.
// See modules/admin/internal/frame_chrome_text_test.go.
const chromeTextSize = "sm"

// Shell is what the manifest hands the implementation.
type Shell struct {
	Nav       []module.NavEntry
	Authorize httpx.Authorizer
	Tenants   tenantcontracts.Service
	// Roles is the auth module's role administration, for the screen that
	// module's nav entry names. Nil mounts no screen, and then Mount reports
	// the entry as unserved, which is what it is.
	Roles Roles
	// Sessions is the auth module's session list, for the screen it serves at
	// /app/auth/sessions. That screen declares no nav entry here: kit/module
	// refuses an entry that names no permission, and the two this module declares
	// would both be wrong for it — so the product that owns the navigation names
	// the entry beside the permission it seeds. Nil mounts no screen, and because
	// nothing declares one, nothing is reported unserved either: a composition
	// with no auth module has no session list to miss.
	Sessions Sessions
	Token    tenancy.SystemToken
	// SignIn is the auth module's session route, which the sign-in form posts
	// to. The composition names it; this module only fills the form's action.
	SignIn string
	// Registration is the composition saying a stranger may make an account
	// here. Nil offers no way in but the password, which is the answer for
	// every composition that wired no registration service.
	Registration *Registration
	// Theme is the installation's two palettes: the one thing about the look of
	// this shell that belongs to whoever runs it. See design.Pair.
	Theme     design.Pair
	Storybook func(context.Context) (export.Storybook, error)
	Messages  page.Messages
	Locale    func(context.Context, page.Request) string
}

// The shell's own addresses. Each is a pair: the relative path the page is
// mounted at, and the address the kernel composed for it. There used to be one
// constant here — adminRoot = "/admin" — and every module's nav entry, the
// failure page, the sign-in link and the stylesheet's own URL repeated it, which
// is what made moving the shell a change in six packages. Nothing outside this
// one writes a prefix now, and the two the composition still quotes (the
// failure page's chrome) come from the addresses below.
type route struct {
	// rel is what the page is mounted at, relative to the surface.
	rel string
	// at is the address it answers at: /app/admin/login and its like.
	at string
}

type addresses struct {
	// workspace is the root of the workspace, whoever serves it.
	workspace route
	// dashboard is this shell's own home: the workspace root when nothing else
	// claimed it, /app/dashboard when a product's home did.
	dashboard route
	assets    route
	login     route
	health    route
	gallery   route
	// tenants and roles are pages this shell writes in another module's
	// namespace, because that is where the workspace puts a module's screens.
	tenants route
	roles   route
	// forgot is this shell's own page — it is the way in, and the way in is the
	// shell's — and reset belongs to auth, whose link the person arrived by.
	forgot route
	reset  route
	// verify is the other half of a sign-up: the link the confirmation mail
	// carries. register is the shell's own page — the way in is the shell's.
	verify   route
	register route
	// sessions is another module's screen again, for the same reason: the auth
	// module names it in its nav, and this is the module that can draw it.
	sessions      route
	sessionRevoke route
	sessionsRest  route
}

// at composes one address for this module's own router.
func at(r *httpx.Router, rel string) route { return route{rel, r.PagePath(rel)} }

// inNamespace composes the address of a page that belongs to the workspace under
// another module's name — the tenant switcher lives beside the tenant screens,
// not beside the shell that renders it — and falls back to the shell's own
// namespace in a composition that has no such module.
func inNamespace(app *httpx.Router, module, rel string) route {
	return at(namespace(app, module), rel)
}

// namespace is the router a page of one module mounts on: the workspace, in
// that module's own namespace — which is where the workspace puts a module's
// screens, whether this shell generated them or wrote them by hand.
func namespace(app *httpx.Router, module string) *httpx.Router {
	if app.Known(module) {
		return app.ForModule(module)
	}
	return app
}

// Mount is the whole shell. Everything a page shares is built here, once, as a
// value — the sheet, the chrome, the navigation, the frame — and then the
// screens, the pages and the catalog are mounted against them. Nothing is
// filled in afterwards.
func Mount(s httpx.Surfaces, sh Shell) {
	sheet := ui.Compose(sh.Theme)
	app := s.App
	// The workspace has one home. This module is composed last, so it takes the
	// claim when no product module wanted the root and the generated dashboard
	// is what a person lands on; a product that claimed it first moves the
	// dashboard one level down, and the shell's own links follow.
	home, claimed := app.Home()
	dashboardRel := "/"
	if !claimed {
		dashboardRel = "/dashboard"
	}
	a := addresses{
		workspace:     at(app, "/"),
		dashboard:     route{dashboardRel, home.PagePath(dashboardRel)},
		assets:        at(app, "/assets"),
		login:         at(app, "/login"),
		health:        at(app, "/health"),
		gallery:       at(app, "/_gallery"),
		tenants:       inNamespace(app, "tenant", "/tenants"),
		roles:         inNamespace(app, "auth", "/roles"),
		forgot:        at(app, "/login/forgot"),
		reset:         inNamespace(app, "auth", "/reset"),
		verify:        inNamespace(app, "auth", "/verify-email"),
		register:      at(app, "/register"),
		sessions:      inNamespace(app, "auth", "/sessions"),
		sessionRevoke: inNamespace(app, "auth", "/sessions/revoke"),
		sessionsRest:  inNamespace(app, "auth", "/sessions/revoke-rest"),
	}
	// Static files are outside every middleware chain: a stylesheet has no
	// tenant, no session and no transaction to pay for.
	app.Static(a.assets.rel, ui.Assets(sheet))

	opts := screens.Options{Workspace: a.workspace.at, Home: "Dashboard"}
	resources := s.Resources()
	sort.Slice(resources, func(i, j int) bool { return resources[i].Screen < resources[j].Screen })

	// What the application answers, asked of the kernel's own recording plus
	// what this mount is about to add — it is composed last, so nothing else
	// will record them. A hand-written page counts as much as a generated one,
	// and there is no second list to keep in step.
	served := page.Served(s.Recorded())
	own := ownScreens(served, resources)
	for _, r := range resources {
		// A Spec that offers no list has no screen address, so it serves no path:
		// recording "" and "/new" would call them addresses something answers.
		if r.Screen != "" {
			served = append(served, r.Screen, r.Screen+"/new")
		}
	}
	served = append(served, a.dashboard.at, a.login.at, a.health.at, a.gallery.at, a.tenants.at)
	if app.Known("auth") {
		// Both are served only when the auth module is composed: a forgot page
		// that posts to a route nobody mounted is a door painted on a wall, and
		// the composition that has no auth module has no password to forget.
		served = append(served, a.forgot.at, a.reset.at, a.verify.at)
	}
	if sh.Registration != nil {
		// The page is served only where the composition offers a door it posts
		// to, which is the same rule that keeps the link off the sign-in card.
		served = append(served, a.register.at)
	}
	if sh.Roles != nil {
		served = append(served, a.roles.at)
	}
	if sh.Sessions != nil {
		served = append(served, a.sessions.at)
	}
	nav := page.NewNavigation(sh.Nav, served, s.Required())
	// A nav entry nothing answers is a mistake in a module's manifest, and it
	// is reported here, once, at boot — not rendered as a disabled row that
	// every person using the application sees for the life of the deployment.
	for _, entry := range nav.Unserved() {
		slog.Default().Warn("admin: a nav entry leads to a path no route serves",
			"label", entry.Label, "screen", entry.Screen, "permission", entry.Permission)
	}

	shell := page.Shell{
		Chrome: page.Chrome{
			Brand: brand, Assets: a.assets.at, Stylesheet: sheet,
			Scripts: ui.Controllers, SignIn: a.login.at,
		},
		Frame:     frame(a, nav, sh.Authorize, sh.storybook),
		Tag:       "admin",
		Back:      a.dashboard.at,
		BackLabel: "Back to the dashboard",
		// The whole shell and not only its sign-in page: a person who signs in in
		// Portuguese and is then handed an English table has not been served in
		// Portuguese. The generated screens read their labels through the
		// negotiation this opts them into (ui/screens' screens.* keys), and a
		// composition that supplies no catalog keeps every one of them in the
		// language they were written in.
		Messages: sh.Messages,
		Locale:   sh.Locale,
	}

	for _, r := range resources {
		if own[r.Screen] {
			// The module wrote this resource's workspace pages itself, and they are what a person is
			// sent to. Generating a second register at the same addresses is a collision the surface
			// gate refuses at boot, so the module's own pages stand and the generated ones are not
			// mounted. The resource keeps its API routes and its catalog entry either way.
			slog.Default().Info("admin: a module serves its own workspace pages for a resource; the generated screens are not mounted",
				"module", r.Module, "entity", r.Entity, "screen", r.Screen)
			continue
		}
		screens.Mount(namespace(app, r.Module), shell, opts, r)
	}
	// The catalogue as the kernel read it off every manifest, taken here because
	// this module is composed last and it is therefore complete. The roles
	// screen offers it as checkboxes; auth checks a write against it. The route
	// itself is the composition's — see app.Options.WorkspaceCatalog.
	pages{Shell: sh, shell: shell, at: a, nav: nav, resources: resources, declared: s.Permissions()}.mount(s, home, app)
}

// frame is the admin's arrangement: the sidebar the caller may follow, the
// header, the body, the footer, and the one dialog every destructive action is
// confirmed in. It closes over the navigation value and the authorizer.
//
// There is no data-theme attribute on the document, and that is the point: the
// stylesheet's dark rules are behind prefers-color-scheme, so a person whose
// system is dark gets dark, and the inline snippet page.Serve adds sets the
// attribute only when they have chosen one for themselves.
func frame(a addresses, nav page.Navigation, authorize httpx.Authorizer, storybook func(context.Context) (export.Storybook, error)) page.Frame {
	return func(ctx context.Context, r page.Request, body []g.Node) g.Node {
		gallery := false
		if r.SignedIn && authorize != nil {
			allowed, err := authorize.Allowed(ctx, r.Tenant, tenancy.Grant{Permission: "gallery:read"})
			if err == nil && allowed {
				_, err = storybook(ctx)
				gallery = err == nil
			}
		}
		// One list of sections for both surfaces: the sidebar from the large breakpoint up, and the
		// header's disclosure below it, where the sidebar is not shown.
		navigation := sidebar(a, nav.Visible(ctx, r.Tenant, authorize), r, gallery)
		return g.Group([]g.Node{
			components.Shell(components.ShellProps{SkipTarget: "content"}, components.ShellSlots{
				Sidebar: []g.Node{components.Sidebar(navigation)},
				Header:  header(r, navigation),
				Main:    body,
				Footer: []g.Node{components.Text(components.TextProps{
					Content: brand + " " + version(), Size: chromeTextSize, Color: "muted"})},
			}),
			components.ConfirmDialog(components.ConfirmDialogProps{Title: "Are you sure?"}),
		})
	}
}

// sidebar is the navigation: the dashboard, what the caller may reach, and the
// two pages about the installation. What the caller may reach is decided
// before this is called — see page.Navigation.Visible — so this renders a
// list and hides nothing of its own.
func sidebar(a addresses, visible []module.NavEntry, r page.Request, gallery bool) components.SidebarProps {
	items := []components.SidebarItem{{Label: "Dashboard", Href: a.dashboard.at, Icon: "gear"}}
	for _, entry := range visible {
		items = append(items, components.SidebarItem{Label: entry.Label, Href: entry.Screen, Icon: "file-text"})
	}
	items = append(items, components.SidebarItem{Label: "Health", Href: a.health.at, Icon: "check-circle"})
	// Use the same permission and composition selection as the direct routes.
	if gallery {
		items = append(items, components.SidebarItem{Label: "Components", Href: a.gallery.at, Icon: "info"})
	}
	// BrandLabel rather than the Brand slot: the sidebar is inverted, and the
	// colour that is legible on it is one the component owns.
	return components.SidebarProps{
		Current: r.Path, NavigationLabel: "Admin navigation", Items: items,
		BrandLabel: fallback(r.Tenant.Name, brand), BrandHref: a.workspace.at,
	}
}

// header is the tenant, the caller, the theme switch and the way out — and, below the large
// breakpoint where the sidebar is not shown, the disclosure that lists the same sections.
func header(r page.Request, navigation components.SidebarProps) []g.Node {
	right := []g.Node{
		components.ButtonWithSlots(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{
				"data-theme-toggle": "", "aria-pressed": "false"}},
			Variant: "ghost", Size: "sm", IconOnly: true,
			AriaLabel: "Switch between the light and dark theme",
		}, components.ButtonSlots{Content: []g.Node{
			components.Icon(components.IconProps{Name: "moon", Size: "sm"})}}),
	}
	if r.SignedIn {
		right = append([]g.Node{
			components.Text(components.TextProps{
				Content: short(r.Principal.UserID.String()) + " · " + fallback(strings.Join(r.Principal.Roles, ", "), "no roles"),
				Size:    chromeTextSize, Color: "muted"}),
		}, right...)
		right = append(right, components.Button(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-sign-out": ""}},
			Label:          "Sign out", Variant: "secondary", Size: "sm",
		}))
	}
	return []g.Node{
		components.Flex(components.FlexProps{Direction: "row", Align: "center", Gap: "3"},
			components.SidebarDisclosure(navigation),
			components.Text(components.TextProps{
				Content: fallback(r.Tenant.Name, brand), Weight: "semibold", Size: chromeTextSize})),
		components.Flex(components.FlexProps{Direction: "row", Align: "center", Gap: "3"}, right...),
	}
}

// version is what the footer says. It is the revision the binary was built
// from, which is a fact the toolchain records; a version string somebody bumps
// by hand is a version string that is wrong.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown build)"
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return short(setting.Value)
		}
	}
	return "(development)"
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func fallback(value, or string) string {
	if strings.TrimSpace(value) == "" {
		return or
	}
	return value
}

// ownScreens is the resources whose workspace a module already serves: a GET a module recorded at the
// resource's screen address or anywhere under it (its row, a command's form). It is asked of the recording
// before this shell adds anything, because the shell is composed last and everything it finds there was
// mounted by a module.
func ownScreens(recorded []string, resources []httpx.Resource) map[string]bool {
	own := map[string]bool{}
	for _, r := range resources {
		// No address of its own: beside an empty screen the prefix is "/", every
		// recorded path starts with it, and the answer would be a lie.
		if r.Screen == "" {
			continue
		}
		for _, path := range recorded {
			if path == r.Screen || strings.HasPrefix(path, r.Screen+"/") {
				own[r.Screen] = true
				break
			}
		}
	}
	return own
}
