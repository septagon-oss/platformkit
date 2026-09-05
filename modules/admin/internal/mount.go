// Package internal is the shell's implementation: one chrome, one frame, the
// five pages written by hand, the catalog, and the screens ui/screens generates
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
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// Shell is what the manifest hands the implementation.
type Shell struct {
	Nav       []module.NavEntry
	Authorize httpx.Authorizer
	Tenants   tenantcontracts.Service
	Token     tenancy.SystemToken
	// Theme is the installation's two palettes: the one thing about the look of
	// this shell that belongs to whoever runs it. See design.Pair.
	Theme design.Pair
}

// adminRoot is where the shell lives; every path below is built from it, so
// moving the shell is one string.
const (
	adminRoot = "/admin"
	// assetPrefix is where ui's stylesheet and controllers are served. They are
	// static files: kit/httpx mounts them beside the API, on the router that
	// carries neither the tenant middleware nor a transaction, so a page load
	// costs one database round trip and not four.
	assetPrefix = adminRoot + "/assets"
	loginPath   = adminRoot + "/login"
	healthPath  = adminRoot + "/health"
	galleryPath = adminRoot + "/_gallery"
	// tenantsPath is where the switcher lives: the path the tenant module's
	// nav entry names.
	tenantsPath = adminRoot + "/tenant/tenants"
	// brand is what the shell calls itself when the tenant has no name.
	brand = "PlatformKit"
)

// opts is where the generated screens live and what their breadcrumb leads
// back to.
var opts = screens.Options{Root: adminRoot, Home: "Dashboard"}

// Mount is the whole shell. Everything a page shares is built here, once, as a
// value — the sheet, the chrome, the navigation, the frame — and then the
// screens, the pages and the catalog are mounted against them. Nothing is
// filled in afterwards.
func Mount(api *httpx.API, s Shell) {
	sheet := ui.Compose(s.Theme)
	api.Static(assetPrefix, ui.Assets(sheet))

	resources := api.Resources()
	sort.Slice(resources, func(i, j int) bool {
		return screens.Path(resources[i], opts) < screens.Path(resources[j], opts)
	})

	// What the application answers, asked of the kernel's own recording plus
	// what this mount is about to add — it is composed last, so nothing else
	// will record them. A hand-written page counts as much as a generated one,
	// and there is no second list to keep in step.
	served := page.Served(api.Recorded())
	for _, r := range resources {
		served = append(served, screens.Path(r, opts), screens.Path(r, opts)+"/new")
	}
	served = append(served, adminRoot, loginPath, healthPath, galleryPath, tenantsPath)
	nav := page.NewNavigation(s.Nav, served, api.Required())
	// A nav entry nothing answers is a mistake in a module's manifest, and it
	// is reported here, once, at boot — not rendered as a disabled row that
	// every person using the application sees for the life of the deployment.
	for _, entry := range nav.Unserved() {
		slog.Default().Warn("admin: a nav entry leads to a path no route serves",
			"label", entry.Label, "path", entry.Path, "permission", entry.Permission)
	}

	shell := page.Shell{
		Chrome: page.Chrome{
			Brand: brand, Assets: assetPrefix, Stylesheet: sheet,
			Scripts: ui.Controllers, SignIn: loginPath,
		},
		Frame:     frame(nav, s.Authorize),
		Tag:       "admin",
		Back:      adminRoot,
		BackLabel: "Back to the dashboard",
	}

	for _, r := range resources {
		screens.Mount(api, shell, opts, r)
	}
	pages{Shell: s, shell: shell, resources: resources}.mount(api)
	mountCatalog(api, resources)
}

// frame is the admin's arrangement: the sidebar the caller may follow, the
// header, the body, the footer, and the one dialog every destructive action is
// confirmed in. It closes over the navigation value and the authorizer.
//
// There is no data-theme attribute on the document, and that is the point: the
// stylesheet's dark rules are behind prefers-color-scheme, so a person whose
// system is dark gets dark, and the inline snippet page.Serve adds sets the
// attribute only when they have chosen one for themselves.
func frame(nav page.Navigation, authorize httpx.Authorizer) page.Frame {
	return func(ctx context.Context, r page.Request, body []g.Node) g.Node {
		return g.Group([]g.Node{
			components.Shell(components.ShellProps{SkipTarget: "content"}, components.ShellSlots{
				Sidebar: []g.Node{sidebar(nav.Visible(ctx, r.Tenant, authorize), r)},
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
func sidebar(visible []module.NavEntry, r page.Request) g.Node {
	items := []components.SidebarItem{{Label: "Dashboard", Href: adminRoot, Icon: "gear"}}
	for _, entry := range visible {
		items = append(items, components.SidebarItem{Label: entry.Label, Href: entry.Path, Icon: "file-text"})
	}
	items = append(items,
		components.SidebarItem{Label: "Health", Href: healthPath, Icon: "check-circle"},
		components.SidebarItem{Label: "Components", Href: galleryPath, Icon: "info"},
	)
	// BrandLabel rather than the Brand slot: the sidebar is inverted, and the
	// colour that is legible on it is one the component owns.
	return components.Sidebar(components.SidebarProps{
		Current: r.Path, NavigationLabel: "Admin navigation", Items: items,
		BrandLabel: fallback(r.Tenant.Name, brand), BrandHref: adminRoot,
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
