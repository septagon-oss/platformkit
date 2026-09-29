package components

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/style"
)

// SidebarDisclosure is the sidebar's navigation for the widths where the
// sidebar itself is not shown. The admin sidebar is `display: none` below the
// large breakpoint, and until this existed nothing on a phone disclosed the
// sections at all (e2e/known-defects.spec.ts recorded it). It renders the same
// items, with the same `data-nav` links and the same `aria-current`, inside a
// native <details>/<summary> — no script, the browser owns the open state and
// the keyboard — and it is hidden from the large breakpoint up, where the
// sidebar takes over. Compose it in the shell's header; it takes the very
// SidebarProps the sidebar does, so the two can never list different sections.
func SidebarDisclosure(p SidebarProps) g.Node {
	label := strings.TrimSpace(p.NavigationLabel)
	if label == "" {
		label = "Admin navigation"
	}
	summary := strings.TrimSpace(p.DisclosureLabel)
	if summary == "" {
		summary = "Menu"
	}
	return h.Details(
		h.Class(clSidebarDisclosure.Compile()),
		g.Attr("data-component", "sidebar-disclosure"),
		h.Summary(h.Class(clSidebarDisclosureSummary.Compile()), g.Text(summary)),
		h.Nav(
			h.Class(clSidebarDisclosurePanel.Compile()),
			g.Attr("aria-label", label),
			sidebarNavigation(p, nil, "content"),
		),
	)
}

var (
	clSidebarDisclosure = style.New().Display(style.DisplayBlock).
				Breakpoint(style.BreakpointLG, func(c style.ClassList) style.ClassList {
			return c.Display(style.DisplayHidden)
		})
	clSidebarDisclosureSummary = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).
					PaddingX(style.S3).PaddingY(style.S2).Rounded(style.RadiusMD).
					Border(style.Border1).BorderColor(style.BorderPrimary).
					FontSize(style.TextSM).FontWeight(style.FontMedium).TextColor(style.FgPrimary).
					Cursor(style.CursorPointer).Merge(clFocusRing)
	clSidebarDisclosurePanel = style.New().MarginTop(style.S2).Padding(style.S2).Rounded(style.RadiusMD).
					Bg(style.SurfacePrimary).Border(style.Border1).BorderColor(style.BorderPrimary)
)
