// Package internal is the shell's implementation: one chrome, one frame, the
// six pages written by hand, the catalog, and the screens ui/screens generates
// for every resource kit/rest registered before this module was composed.
package internal

import (
	"context"
	"fmt"
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
	"github.com/septagon-oss/platformkit/ui/resource"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// brand is what the shell calls itself when the tenant has no name.
const brand = "PlatformKit"

// Shell is what the manifest hands the implementation.
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

// chromeTextSize is the one body step the frame's own text takes: the tenant name, the caller and the
// build stamp are the chrome, and the design floor allows a page two body sizes in total — so three
// sizes in the chrome alone leaves the page's own copy nothing. `sm` is what two of the three already
// took, and it is the sidebar's own nav step, which leaves `base` free for the page inside <main>.
const chromeTextSize = "sm"

type Shell struct {
	Nav []module.NavEntry
	// Primary is the action the home leads with, filtered like a nav entry. The
	// zero value draws none.
	Primary   module.NavEntry
	Authorize httpx.Authorizer
	Tenants   tenantcontracts.Service
	// TenantLanguages is the tenant's own declaration of the languages it speaks,
	// the door the Languages page reads and writes. It is beside Tenants rather than
	// a method on it because contracts.LanguageDeclaration is a second interface:
	// which languages a customer is served in is written from inside that customer's
	// transaction, and the control plane's read of the same fact is a different
	// capability held by a different person.
	TenantLanguages tenantcontracts.LanguageDeclaration
	// Roles is the auth module's role administration, for the screen that
	// module's nav entry names. Nil mounts no screen, and then Mount reports
	// the entry as unserved, which is what it is.
	Roles Roles
	Token tenancy.SystemToken
	// SignIn is the auth module's session route, which the sign-in form posts
	// to. The composition names it; this module only fills the form's action.
	SignIn string
	// Language is the account command's other half: the shell reads a person's
	// own language to draw the control in the chrome and hands their choice to the
	// module that owns people. Nil mounts no control at all — a composition with no
	// user module has no account to speak of, and the header shows the sign-out it
	// has always shown. See AccountLanguage.
	Language AccountLanguage
	// LanguageVersion is the same door counted: the account control draws the
	// revision its choice was read at and posts it back, so an old page cannot write
	// an old choice over a new one. Nil draws no control over a form that cannot keep
	// that promise; see AccountLanguageVersion.
	LanguageVersion AccountLanguageVersion
	// Installed are the languages this installation's copy is written in, which are
	// the most a tenant may be served in. The composition read them off the
	// catalogues it composed; the languages page offers them and no other, because a
	// page offering a language no catalogue carries is a page that lies about its own
	// text. Empty offers nothing, which is the truth about a composition with no copy.
	Installed []string
	// Sessions is the auth module's session list, for the screen it serves at
	// /app/auth/sessions. That screen declares no nav entry here: kit/module
	// refuses an entry that names no permission, and the two this module declares
	// would both be wrong for it — so the product that owns the navigation names
	// the entry beside the permission it seeds. Nil mounts no screen, and because
	// nothing declares one, nothing is reported unserved either: a composition
	// with no auth module has no session list to miss.
	Sessions Sessions
	// People is the caller's own account row, for the one header line that names
	// them. Nil — the default — draws "Signed in" instead of a name: a frame that
	// cannot ask still renders, and an id fragment is not a name.
	//
	// Unlike Roles it mounts no screen and so reports nothing when absent: the
	// header is not a door a nav entry leads to.
	People People
	// PictureSource is the address template a generated picture cell fetches its
	// bytes from, handed straight to the screen renderer that draws the cell. The
	// composition names it for the same reason it names SignIn: the door belongs
	// to the module that stores the bytes. Empty draws the plain value.
	PictureSource string
	// ComposedSource is the composed-picture address with a {reference} placeholder.
	ComposedSource string
	// Registration is the composition saying a stranger may make an account
	// here. Nil offers no way in but the password, which is the answer for
	// every composition that wired no registration service.
	Registration *Registration
	// languagePath is languageRel composed onto this shell's own namespace, filled
	// in by Mount: the account form's action, and nothing else.
	languagePath string
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
	// languages is the tenant's own settings page, written here and mounted in the
	// tenant module's namespace for the same reason the switcher is.
	languages route
	// tenants and roles are pages this shell writes in another module's
	// namespace, because that is where the workspace puts a module's screens.
	tenants route
	roles   route
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
	// The account form's action, composed here rather than written: the shell owns
	// its own prefix and nothing above it (see addresses).
	sh.languagePath = at(app, languageRel).at

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
		languages:     inNamespace(app, "tenant", "/languages"),
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

	opts := screens.Options{
		Workspace: a.workspace.at, Home: "Dashboard",
		PictureSource:  sh.PictureSource,
		ComposedSource: sh.ComposedSource,
	}
	resources := s.Resources()
	sort.Slice(resources, func(i, j int) bool { return resources[i].Screen < resources[j].Screen })

	// What the application answers, asked of the kernel's own recording plus
	// what this mount is about to add — it is composed last, so nothing else
	// will record them. A hand-written page counts as much as a generated one,
	// and there is no second list to keep in step.
	// What is already answered, and what this shell is about to write by hand. The
	// two are one list for the question a nav entry asks — does anything answer
	// there? — and they have to be read together for the second question too:
	// a resource whose screen this shell writes itself gets no generated second one.
	// Reading only what is *already* recorded asked that question too early, because
	// the hand-written pages are mounted below, so the resource a module wrote its
	// own page for looked unclaimed and a register nobody asked for was mounted in
	// front of it — which the surface gate refuses at boot.
	recorded := append(page.Served(s.Recorded()),
		a.dashboard.at, a.login.at, a.health.at, a.gallery.at, a.tenants.at, a.languages.at)
	if sh.Language != nil {
		recorded = append(recorded, sh.languagePath)
	}
	if app.Known("auth") {
		// Both are served only when the auth module is composed: a forgot page
		// that posts to a route nobody mounted is a door painted on a wall, and
		// the composition that has no auth module has no password to forget.
		recorded = append(recorded, a.forgot.at, a.reset.at, a.verify.at)
	}
	if sh.Registration != nil {
		// The page is served only where the composition offers a door it posts
		// to, which is the same rule that keeps the link off the sign-in card.
		recorded = append(recorded, a.register.at)
	}
	if sh.Roles != nil {
		recorded = append(recorded, a.roles.at)
	}
	if sh.Sessions != nil {
		recorded = append(recorded, a.sessions.at)
	}
	own := ownScreens(recorded, resources)
	served := recorded
	for _, r := range resources {
		// A Spec that offers no list has no screen address, so it serves no path:
		// recording "" and "/new" would call them addresses something answers.
		if r.Screen != "" {
			served = append(served, r.Screen, r.Screen+"/new")
		}
	}
	nav := page.NewNavigation(sh.Nav, served, s.Required())
	// A nav entry nothing answers is a mistake in a module's manifest, and it
	// is reported here, once, at boot — not rendered as a disabled row that
	// every person using the application sees for the life of the deployment.
	for _, entry := range nav.Unserved() {
		slog.Default().Warn("admin: a nav entry leads to a path no route serves",
			"label", entry.Label, "screen", entry.Screen, "permission", entry.Permission)
	}
	var primary page.Navigation
	if sh.Primary.Screen != "" {
		primary = page.NewNavigation([]module.NavEntry{sh.Primary}, served, s.Required())
		for _, entry := range primary.Unserved() {
			slog.Default().Warn("admin: the primary action leads to a path no route serves",
				"label", entry.Label, "screen", entry.Screen, "permission", entry.Permission)
		}
	}

	frameAt := a
	if sh.Sessions == nil {
		frameAt.sessions = route{}
	}
	shell := page.Shell{
		Chrome: page.Chrome{
			Brand: brand, Assets: a.assets.at, Stylesheet: sheet,
			Scripts: ui.Controllers, SignIn: a.login.at,
		},
		Frame: frame(frameAt, nav, frameDeps{
			Authorize: sh.Authorize, Storybook: sh.storybook, Resources: resources,
			People: sh.People, Language: sh.Language, LanguageVersion: sh.LanguageVersion,
			LanguagePath: sh.languagePath,
		}),
		Tag:       "admin",
		Back:      a.dashboard.at,
		BackLabel: "Back to the dashboard",
		// The words of that way out are the shell's copy too, so they are asked at
		// this module's catalogue and not left in the language this file is written in.
		BackKey: "admin.back",
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
	pages{Shell: sh, shell: shell, at: a, nav: nav, primary: primary, resources: resources, declared: s.Permissions()}.mount(s, home, app)
}

// frame is the admin's arrangement: the sidebar the caller may follow, the
// header, the body, the footer, and the one dialog every destructive action is
// confirmed in. It closes over the navigation value and the authorizer.
//
// There is no data-theme attribute on the document, and that is the point: the
// stylesheet's dark rules are behind prefers-color-scheme, so a person whose
// system is dark gets dark, and the inline snippet page.Serve adds sets the
// attribute only when they have chosen one for themselves.
// frameDeps is what the frame closes over, as values: who may be asked whether a
// caller is in, which composition the gallery link is worth offering for, the
// resources the workspace groups by, and the account command the language control
// reads and writes. A bag rather than six parameters because the frame is built
// once and read on every request, and a positional list that long is a call nobody
// can read.
type frameDeps struct {
	Authorize       httpx.Authorizer
	Storybook       func(context.Context) (export.Storybook, error)
	Resources       []httpx.Resource
	People          People
	Language        AccountLanguage
	LanguageVersion AccountLanguageVersion
	LanguagePath    string
}

func frame(a addresses, nav page.Navigation, deps frameDeps) page.Frame {
	return func(ctx context.Context, r page.Request, body []g.Node) g.Node {
		gallery := false
		if r.SignedIn && deps.Authorize != nil {
			allowed, err := deps.Authorize.Allowed(ctx, r.Tenant, tenancy.Grant{Permission: "gallery:read"})
			if err == nil && allowed {
				_, err = deps.Storybook(ctx)
				gallery = err == nil
			}
		}
		// One list of sections for both surfaces: the sidebar from the large breakpoint up, and the
		// header's disclosure below it, where the sidebar is not shown.
		// A tenant's own languages, offered to whoever the authorizer says may set
		// them: the same question the page's guard asks, asked once here so the link
		// and the door cannot disagree.
		maySetLanguages := false
		if r.SignedIn && deps.Authorize != nil {
			allowed, err := deps.Authorize.Allowed(ctx, r.Tenant,
				tenancy.Grant{Permission: tenantcontracts.PermissionLocaleManage})
			maySetLanguages = err == nil && allowed
		}
		navigation := sidebar(a, nav.Visible(ctx, r.Tenant, deps.Authorize), r, gallery, maySetLanguages, deps.Resources...)
		line := ""
		if r.SignedIn {
			// A name is a fact about a row and arrives from the account port. The
			// absence of one is a sentence about the session, and a sentence is asked
			// of the catalogue in the language this page is written in rather than
			// written here: an English fallback would greet a Portuguese reader in
			// English whenever the account holds no name and no address.
			line = caller(ctx, deps.People, r.Principal.UserID)
			if line == "" {
				line = chrome(r, "workspace.signed_in", "Signed in")
			}
		}
		return g.Group([]g.Node{
			components.Shell(components.ShellProps{
				SkipTarget: "content",
				SkipLabel:  chrome(r, "shell.skip", "Skip to content"),
			}, components.ShellSlots{
				Sidebar: []g.Node{components.Sidebar(navigation)},
				Header:  header(ctx, deps, r, navigation, line),
				Main:    body,
				Footer:  []g.Node{about(r)},
			}),
			components.ConfirmDialog(components.ConfirmDialogProps{
				Title:       chrome(r, "dialog.title", "Are you sure?"),
				CancelLabel: chrome(r, "dialog.cancel", "Cancel"),
				AcceptLabel: chrome(r, "dialog.delete", "Delete")}),
		})
	}
}

// navLabel is a module's nav entry, in the language this request is answered in.
//
// The entry's Label is the module's own sentence, written where the module declares
// the screen. The key comes from the entry's Screen and never from its Label,
// because a label is exactly what a translation is allowed to change and a screen
// is not. The words belong to the module that declared the entry — nav.user.users
// is answered in modules/user's catalogue, and nothing in this package holds it —
// which is the rule a permission label already follows. An entry nobody translated
// is answered in the language its module wrote it in.
//
// The exception is the two pages this shell writes inside another module's
// namespace, the tenant switcher and the roles screen (writtenHere). The word they
// are led by comes from this package's own catalogue rather than the declaring
// module's, because the copy of a page belongs to whoever wrote the page, and
// because the alternative is a menu that renames the screen it leads to: a heading
// read out of one catalogue and a link out of another drift the moment either is
// re-worded. The entry's own label is the last resort, so a composition with no
// catalogue at all still shows what its manifest declared.
func navLabel(r page.Request, entry module.NavEntry, own string) string {
	fallback := entry.Label
	if own != "" {
		fallback = own
	}
	if r.Locale == nil {
		return fallback
	}
	key := "nav." + strings.ReplaceAll(
		strings.TrimPrefix(entry.Screen, httpx.AppRoot+"/"), "/", ".")
	return r.Locale.Text(key, fallback)
}

// writtenHere is this shell's own word for a nav entry whose screen it serves with a
// page it wrote, and empty for every other entry. The two are the pages mounted in
// another module's namespace — see addresses.tenants and addresses.roles — and the
// key is the one the page itself is titled with, so a person who follows the link
// arrives at the words the link used.
func (a addresses) writtenHere(screen string) (string, bool) {
	switch screen {
	case a.roles.at:
		return "roles.title", true
	case a.tenants.at:
		return "tenants.title", true
	}
	return "", false
}

// sidebar is the navigation: the dashboard, what the caller may reach, and the
// two pages about the installation. What the caller may reach is decided
// before this is called — see page.Navigation.Visible — so this renders a
// list and hides nothing of its own.
func sidebar(a addresses, visible []module.NavEntry, r page.Request, gallery, maySetLanguages bool, resources ...httpx.Resource) components.SidebarProps {
	o := resource.Options{Locale: r.Locale}
	sections := []components.SidebarSection{{ID: "overview", Items: []components.SidebarItem{
		{Label: o.Text("admin.workspace.dashboard", "Dashboard"), Href: a.dashboard.at, Icon: "gear"},
	}}}
	work := components.SidebarSection{ID: "workspace", Label: o.Text("admin.workspace.workspace", "Workspace")}
	for _, group := range workspaceSections(a, visible, resources, o) {
		if group.ID == "platform" {
			sections = append(sections, group)
		} else {
			work.Items = append(work.Items, group.Items...)
		}
	}
	// A tenant's own languages, in its own namespace, offered to whoever the
	// authorizer says may set them — the same answer the page's guard gives, so the
	// link and the door cannot disagree.
	if maySetLanguages {
		work.Items = append(work.Items, components.SidebarItem{
			Label: o.Text("admin.nav.languages", "Languages"), Href: a.languages.at, Icon: "globe"})
	}
	if len(work.Items) > 0 {
		sections = append(sections, work)
	}
	label := o.Text("admin.workspace.support", "Support")
	if r.Tenant.Operator {
		label = o.Text("admin.workspace.system", "System")
	}
	sections = append(sections, components.SidebarSection{ID: "system", Label: label,
		Items: []components.SidebarItem{{Label: o.Text("admin.workspace.health", "Health"), Href: a.health.at, Icon: "check-circle"}},
	})
	if gallery {
		sections = append(sections, components.SidebarSection{ID: "developer", Label: o.Text("admin.workspace.developer", "Developer tools"),
			Items: []components.SidebarItem{{Label: o.Text("admin.workspace.components", "Components"), Href: a.gallery.at, Icon: "info"}},
		})
	}
	if r.SignedIn && a.sessions.at != "" {
		sections = append(sections, components.SidebarSection{ID: "account", Label: o.Text("admin.workspace.account", "Your account"), Items: []components.SidebarItem{{Label: o.Text("admin.workspace.sessions", "Sessions"), Href: a.sessions.at, Icon: "user"}}})
	}
	// A module's own navigation label was asked of the reader's catalogue where
	// workspaceSections grouped it — see entryWord — so every label arriving here is
	// already in the language this request resolved to. The frame's own destinations,
	// Dashboard, Health and Sessions, are titled by the admin.workspace.* keys above,
	// which name them by what this shell serves rather than by the module behind an
	// address.
	return components.SidebarProps{
		Current: r.Path, NavigationLabel: o.Text("admin.workspace.navigation", "Admin navigation"), Sections: sections,
		DisclosureLabel: chrome(r, "nav.menu", "Menu"),
		BrandLabel:      workspace(r, brand), BrandHref: a.workspace.at,
	}
}

// chrome is a word of the shell's own copy, in the language this request is being
// answered in. Every string the frame, the sidebar and the header write used to be
// a Go literal, which meant a tenant served in Portuguese got a page that declared
// pt-PT — because the pages inside the frame read their labels from the catalogue —
// while its navigation, its sign-out and its confirmation dialog stayed English.
// That is worse than an English page, which at least declares English: the
// document promises one language to a screen reader and a translation tool and
// speaks another. The fallback is the source English, so a composition with no
// catalogue still says what it always said, and the key list belongs to
// modules/admin/messages, in both of its files.
func chrome(r page.Request, key, fallback string) string {
	if r.Locale == nil {
		return fallback
	}
	return r.Locale.Text("admin."+key, fallback)
}

// chromeSentence is the shell's own copy with somebody else's words inside it — a
// role's name, the permissions a save would drop. The placeholders belong to the
// catalogue entry, so a translation puts the name where its own sentence reads it
// rather than where this file happened to concatenate it, and the fallback is the
// source English the call site is written in.
func chromeSentence(r page.Request, key, format string, args ...any) string {
	if r.Locale == nil {
		return fmt.Sprintf(format, args...)
	}
	return r.Locale.Text("admin."+key, format, args...)
}

// chromeCount is a sentence of the shell's own copy about how many there are, in
// the form this request's language picks for that number. English writes "1 role."
// and "4 roles."; Portuguese writes "1 função." and "4 funções.". Neither is built
// here by appending a letter to a word — the two forms are the catalogue's, and the
// language's cardinal rule chooses between them — which is what tools/checkplurals
// refuses a page for doing by hand.
func chromeCount(r page.Request, key string, count int64, one, other string) string {
	if r.Locale == nil {
		chosen := other
		if count == 1 {
			chosen = one
		}
		return fmt.Sprintf(chosen, count)
	}
	return r.Locale.Plural("admin."+key, count, one, other)
}

// workspace is what this installation is called: the tenant's own name, or the
// host the request arrived at, or — when the request arrived at no host anybody
// could show — the installation's name, which is at least true.
//
// It is said once on a screen at a time. The sidebar paints it from the large
// breakpoint up and paints nothing below it; components.ChromeContext is its
// mirror and is hidden from that breakpoint up, so a phone reader knows which
// workspace they are in and a desktop reader is not told twice.
func workspace(r page.Request, brand string) string {
	return fallback(r.Tenant.Name, hostLabel(r.Host, brand))
}

// hostLabel is a Host header as a name. It is the one string on this page that
// came from the request rather than from the database, and a Host header is what
// anybody sends, so anything outside a host grammar answers the fallback: the
// installation's own name.
//
// *http.Request.Host is already scheme-free and path-free; what is left to refuse
// is a path or query smuggled in, credentials, whitespace, a port that is not
// digits, and a name longer than DNS allows.
func hostLabel(host, or string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || len(host) > 253 {
		return or
	}
	if at := strings.LastIndexByte(host, ':'); at >= 0 {
		port := host[at+1:]
		if !onlyDigits(port) {
			return or // a colon that does not precede digits is not a port
		}
		if port == "80" || port == "443" {
			host = host[:at] // the default port for the scheme says nothing a reader needs
		}
	}
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		default:
			return or
		}
	}
	if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return or
	}
	return host
}

func onlyDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// header is the workspace, the caller, the theme switch and the way out — and, below the large
// breakpoint where the sidebar is not shown, the disclosure that lists the same sections.
func header(ctx context.Context, deps frameDeps, r page.Request, navigation components.SidebarProps, line string) []g.Node {
	o := resource.Options{Locale: r.Locale}
	right := []g.Node{
		components.ButtonWithSlots(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{
				"data-theme-toggle": "", "aria-pressed": "false"}},
			Variant: "ghost", Size: "sm", IconOnly: true,
			AriaLabel: o.Text("admin.workspace.theme", "Switch between the light and dark theme"),
		}, components.ButtonSlots{Content: []g.Node{
			components.Icon(components.IconProps{Name: "moon", Size: "sm"})}}),
	}
	if control := languageControl(ctx, deps, r); control != nil {
		right = append(right, control)
	}
	if r.SignedIn {
		// The person, by the name they chose or the address they signed in with.
		// Never an id fragment: "8258e4cc · admin" is what a machine knows this
		// account by, printed where a greeting should be.
		//
		// The roles leave this line. They stay where they are authoritative and
		// readable — /app/auth/roles, and the sidebar the person is looking at is
		// already the filtered answer of the same grants. Keeping them here would
		// make the no-name fallback read "admin", a role standing where a person's
		// name should be, and would put a second fact on a line this frame measures
		// as one.
		right = append([]g.Node{
			components.Text(components.TextProps{Element: "span", Content: line, Size: chromeTextSize, Color: "muted"}),
		}, right...)
		right = append(right, components.Button(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-sign-out": ""}},
			Label:          o.Text("admin.workspace.sign_out", "Sign out"), Variant: "secondary", Size: "sm",
		}))
	}
	return []g.Node{
		components.Flex(components.FlexProps{Direction: "row", Align: "center", Gap: "3"},
			components.SidebarDisclosure(navigation),
			components.ChromeContext(components.ChromeContextProps{Name: workspace(r, brand), Size: chromeTextSize})),
		// The line wraps rather than grows. It carries the person's own name, the theme
		// switch, the language control and the way out, and what the person can call
		// themselves is nobody else's business: nothing here may be cut off at a narrow
		// width, and nothing here may push the page wider than the screen it is read on.
		components.Flex(components.FlexProps{Direction: "row", Align: "center", Gap: "3", Wrap: true}, right...),
	}
}

// about is the footer: the build stamp, always in view, and behind one closed
// disclosure the fact support reads and a person does not look for — whose
// workspace the host serves — so the header is left to name the person. Signed in
// or not, as the build stamp always was.
func about(r page.Request) g.Node {
	o := resource.Options{Locale: r.Locale}
	return g.Group([]g.Node{
		components.Text(components.TextProps{Element: "span", Content: brand + " " + version(), Size: chromeTextSize, Color: "muted"}),
		components.Disclosure(components.DisclosureProps{Label: o.Text("admin.workspace.about", "About this workspace"), Flush: true},
			components.Text(components.TextProps{Element: "span", Content: workspaceScope(r.Tenant, o), Size: chromeTextSize, Color: "muted"})),
	})
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
		if r.Screen == "" {
			// A resource with no screen address serves no workspace page. Beside an
			// empty address every recorded path looks like it sits beneath one, and
			// the boot log would name pages a module does not serve.
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
