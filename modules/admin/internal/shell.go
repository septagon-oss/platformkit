package internal

import (
	"context"
	"errors"
	"net/http"
	"runtime/debug"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
)

// adminRoot is where the shell lives. Every path below is built from it, so
// moving the shell is one string.
const adminRoot = "/admin"

// assetPrefix is where ui's stylesheet and controllers are served. They are
// static files: kit/httpx mounts them beside the API, on the router that
// carries neither the tenant middleware nor a transaction, so a page load costs
// one database round trip and not four.
const assetPrefix = adminRoot + "/assets"

// beforePaint sets the theme from what the person last chose, before the
// stylesheet applies. It has to be inline and it has to be here: a theme
// applied by a deferred script is a page that flashes white on the way to dark.
//
// Inline is also the one thing the content security policy forbids, so the tag
// carries the request's nonce — httpx.Script is the shape that cannot be
// written without one. A nonce rather than a file because a file cannot be it:
// this has to run before the first paint, so it cannot be deferred, and a
// blocking <script src> in the head is a round trip on every page load to save
// four lines.
const beforePaint = `try{var t=localStorage.getItem("platformkit-theme");if(t)document.documentElement.setAttribute("data-theme",t)}catch(e){}`

// page renders a whole document around a screen: the head, the frame, the
// navigation the caller is allowed to see, and the one confirm dialog.
//
// There is no data-theme attribute on the document, and that is the point: the
// stylesheet's dark rules are behind prefers-color-scheme, so a person whose
// system is dark gets dark, and beforePaint sets the attribute only when they
// have chosen one for themselves. Writing data-theme="light" here — which is
// what this did — pinned every page to light and made the toggle the only way
// to be asked. bare() never had one, which is why the sign-in page was the one
// screen that obeyed the operating system.
func (s *Shell) page(ctx context.Context, title string, body ...g.Node) g.Node {
	return s.frame(ctx, title, nil, body...)
}

// frame is page with something extra in the head. There is one caller: the
// gallery, which is the only screen that renders components no other page does
// and so the only one that links the second stylesheet. See ui.GalleryStylesheet.
func (s *Shell) frame(ctx context.Context, title string, extraHead []g.Node, body ...g.Node) g.Node {
	tenant, _ := tenancy.FromContext(ctx)
	return h.HTML(h.Lang("en"),
		s.head(ctx, title+" · "+fallback(tenant.Name, "PlatformKit"), extraHead...),
		h.Body(
			components.Shell(components.ShellProps{SkipTarget: "content"}, components.ShellSlots{
				Sidebar: []g.Node{s.sidebar(ctx)},
				Header:  s.header(ctx, tenant),
				Main:    body,
				Footer:  []g.Node{components.Text(components.TextProps{Content: "PlatformKit " + version(), Size: "xs", Color: "muted"})},
			}),
			components.ConfirmDialog(components.ConfirmDialogProps{Title: "Are you sure?"}),
		),
	)
}

// head is every page's head: the stylesheet with its fingerprint, the inline
// theme snippet, and ui.Controllers as deferred scripts in order — deferred, so
// the document parses before any of them runs and the order is still theirs.
func (s *Shell) head(ctx context.Context, title string, extra ...g.Node) g.Node {
	scripts := make([]g.Node, 0, len(ui.Controllers))
	for _, name := range ui.Controllers {
		scripts = append(scripts, h.Script(h.Src(assetPrefix+"/js/"+name), g.Attr("defer")))
	}
	return h.Head(
		h.Meta(h.Charset("utf-8")),
		h.Meta(h.Name("viewport"), h.Content("width=device-width, initial-scale=1")),
		h.Meta(h.Name("color-scheme"), h.Content("light dark")),
		h.TitleEl(g.Text(title)),
		h.Link(h.Rel("stylesheet"), h.Href(assetPrefix+"/app.css?v="+s.Sheet.Fingerprint)),
		httpx.Script(ctx, beforePaint),
		g.Group(scripts),
		g.Group(extra),
	)
}

// header is the tenant, the caller, the theme switch and the way out.
func (s *Shell) header(ctx context.Context, tenant tenancy.Tenant) []g.Node {
	right := []g.Node{
		components.ButtonWithSlots(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{
				"data-theme-toggle": "", "aria-pressed": "false"}},
			Variant: "ghost", Size: "sm", IconOnly: true,
			AriaLabel: "Switch between the light and dark theme",
		}, components.ButtonSlots{Content: []g.Node{
			components.Icon(components.IconProps{Name: "moon", Size: "sm"})}}),
	}
	if p, ok := tenancy.PrincipalFrom(ctx); ok {
		right = append([]g.Node{
			components.Text(components.TextProps{
				Content: short(p.UserID.String()) + " · " + fallback(strings.Join(p.Roles, ", "), "no roles"),
				Size:    "sm", Color: "muted"}),
		}, right...)
		right = append(right, components.Button(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-sign-out": ""}},
			Label:          "Sign out", Variant: "secondary", Size: "sm",
		}))
	}
	return []g.Node{
		components.Text(components.TextProps{Content: fallback(tenant.Name, "PlatformKit"), Weight: "semibold"}),
		components.Flex(components.FlexProps{Direction: "row", Align: "center", Gap: "3"}, right...),
	}
}

// sidebar is every module's navigation, filtered to what the caller may reach.
//
// The filter asks the same authorizer the kernel enforces with, so a link that
// is shown is a link that works — and an entry no route answers is not shown at
// all. It used to be rendered disabled, which put a module's wiring mistake in
// front of every person who uses the application, every day, in a place they
// can do nothing about. It is a fact about the composition, so it is reported
// once where the composition happens: see Mount.
func (s *Shell) sidebar(ctx context.Context) g.Node {
	tenant, hasTenant := tenancy.FromContext(ctx)
	current := ""
	if r, ok := httpx.RequestFrom(ctx); ok {
		current = r.URL.Path
	}
	items := []components.SidebarItem{{Label: "Dashboard", Href: adminRoot, Icon: "gear"}}
	for _, entry := range s.Nav {
		// An operator screen is not shown to a customer, for the same reason
		// the kernel refuses it before it asks the Authorizer: the wildcard an
		// administrator holds in their own tenant is not a claim on everybody
		// else's. The grant carries the flag the route declared, so the
		// Authorizer is asked the same question the kernel would ask.
		// See httpx.OperatorPermission.
		grant := tenancy.Grant{Permission: entry.Permission, Operator: s.operator[entry.Permission]}
		if grant.Operator && !tenant.Operator {
			continue
		}
		if hasTenant {
			allowed, err := s.Authorize.Allowed(ctx, tenant, grant)
			if err != nil || !allowed {
				continue
			}
		}
		if !s.served[entry.Path] {
			continue
		}
		items = append(items, components.SidebarItem{
			Label: entry.Label, Href: entry.Path, Icon: "file-text",
		})
	}
	items = append(items,
		components.SidebarItem{Label: "Health", Href: adminRoot + "/health", Icon: "check-circle"},
		components.SidebarItem{Label: "Components", Href: adminRoot + "/_gallery", Icon: "info"},
	)
	// BrandLabel rather than the Brand slot: the sidebar is inverted, and the
	// colour that is legible on it is one the component owns.
	return components.Sidebar(components.SidebarProps{
		Current: current, NavigationLabel: "Admin navigation", Items: items,
		BrandLabel: fallback(tenant.Name, "PlatformKit"), BrandHref: adminRoot,
	})
}

// fault turns a handler's error into a page a person can read, keeping the
// status so the kernel still rolls the transaction back and a probe still sees
// what happened. A 5xx keeps kit/problem's silence: the reason is in the log,
// with the request id this page does not print.
func (s *Shell) fault(ctx context.Context, err error) (*httpx.Page, error) {
	status, detail := http.StatusInternalServerError, ""
	var p *problem.Problem
	if errors.As(err, &p) {
		status, detail = p.Status, p.Detail
	}
	if status >= http.StatusInternalServerError {
		return nil, err // the kernel's problem document, and the log line behind it
	}
	body := s.page(ctx, http.StatusText(status),
		components.Toolbar(components.ToolbarProps{Title: http.StatusText(status)}),
		components.Alert(components.AlertProps{
			Tone: "danger", Message: fallback(detail, "That did not work."), Bordered: true}),
		components.Link(components.LinkProps{Label: "Back to the dashboard", Href: adminRoot}),
	)
	return httpx.Document(body, status)
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
