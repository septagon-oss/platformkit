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

// brand is what the shell calls itself when the tenant has no name.
const brand = "PlatformKit"

// Shell is what the manifest hands the implementation.
type Shell struct {
	Nav       []module.NavEntry
	Authorize httpx.Authorizer
	Tenants   tenantcontracts.Service
	// Roles is the auth module's role administration, for the screen that
	// module's nav entry names. Nil mounts no screen, and then Mount reports
	// the entry as unserved, which is what it is.
	Roles Roles
	Token tenancy.SystemToken
	// SignIn is the auth module's session route, which the sign-in form posts
	// to. The composition names it; this module only fills the form's action.
	SignIn string
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
		workspace: at(app, "/"),
		dashboard: route{dashboardRel, home.PagePath(dashboardRel)},
		assets:    at(app, "/assets"),
		login:     at(app, "/login"),
		health:    at(app, "/health"),
		gallery:   at(app, "/_gallery"),
		tenants:   inNamespace(app, "tenant", "/tenants"),
		roles:     inNamespace(app, "auth", "/roles"),
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
	for _, r := range resources {
		served = append(served, r.Screen, r.Screen+"/new")
	}
	served = append(served, a.dashboard.at, a.login.at, a.health.at, a.gallery.at, a.tenants.at)
	if sh.Roles != nil {
		served = append(served, a.roles.at)
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
	}

	for _, r := range resources {
		screens.Mount(namespace(app, r.Module), shell, opts, r)
	}
	// The catalogue as the kernel read it off every manifest, taken here because
	// this module is composed last and it is therefore complete. The roles
	// screen offers it as checkboxes; auth checks a write against it. The route
	// itself is the composition's — see app.Options.WorkspaceCatalog.
	pages{Shell: sh, shell: shell, at: a, resources: resources, declared: s.Permissions()}.mount(s, home, app)
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
		return g.Group([]g.Node{
			components.Shell(components.ShellProps{SkipTarget: "content"}, components.ShellSlots{
				Sidebar: []g.Node{sidebar(a, nav.Visible(ctx, r.Tenant, authorize), r, gallery)},
				Header:  header(r),
				Main:    body,
				Footer: []g.Node{components.Text(components.TextProps{
					Content: brand + " " + version(), Size: "xs", Color: "muted"})},
			}),
			components.ConfirmDialog(components.ConfirmDialogProps{Title: "Are you sure?"}),
		})
	}
}

// sidebar is the navigation: the dashboard, what the caller may reach, and the
// two pages about the installation. What the caller may reach is decided
// before this is called — see page.Navigation.Visible — so this renders a
// list and hides nothing of its own.
func sidebar(a addresses, visible []module.NavEntry, r page.Request, gallery bool) g.Node {
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
	return components.Sidebar(components.SidebarProps{
		Current: r.Path, NavigationLabel: "Admin navigation", Items: items,
		BrandLabel: fallback(r.Tenant.Name, brand), BrandHref: a.workspace.at,
	})
}

// header is the tenant, the caller, the theme switch and the way out.
func header(r page.Request) []g.Node {
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
				Size:    "sm", Color: "muted"}),
		}, right...)
		right = append(right, components.Button(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-sign-out": ""}},
			Label:          "Sign out", Variant: "secondary", Size: "sm",
		}))
	}
	return []g.Node{
		components.Text(components.TextProps{Content: fallback(r.Tenant.Name, brand), Weight: "semibold"}),
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
