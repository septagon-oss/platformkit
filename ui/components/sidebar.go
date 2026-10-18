package components

// sidebar.go renders the navigation rail: brand, sections, items, and which item
// the current address makes active.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/style"
)

// SidebarSlots carries trusted rich composition around the portable navigation
// model. Items is a compatibility seam for already-rendered navigation nodes;
// new portable callers should prefer SidebarProps.Items or Sections.
type SidebarSlots struct {
	Brand  []g.Node
	Items  []g.Node
	Footer []g.Node
}

// Sidebar renders the portable sidebar model.
func Sidebar(p SidebarProps) g.Node {
	return SidebarWithSlots(p, SidebarSlots{})
}

// SidebarWithSlots renders the canonical persistent navigation surface while
// preserving rich brand and footer composition for trusted Go callers.
func SidebarWithSlots(p SidebarProps, slots SidebarSlots) g.Node {
	flavor := "admin"
	if p.Flavor == "content" {
		flavor = "content"
	}
	collapsible := p.Collapsible || p.Collapsed

	rootClass := clSidebarRootAdmin
	columnClass := clSidebarColumnAdmin
	brandClass := clSidebarBrandAdmin
	navWrapClass := clSidebarNavWrapAdmin
	navClass := clSidebarNavAdmin
	footerClass := clSidebarFooterAdmin
	if flavor == "content" {
		rootClass = clSidebarRootContent
		columnClass = clSidebarColumnContent
		brandClass = clSidebarBrandContent
		navWrapClass = clSidebarNavWrapContent
		navClass = clSidebarNavContent
	} else if p.Collapsed {
		rootClass = rootClass.Merge(clSidebarWidthCollapsed)
	} else {
		rootClass = rootClass.Merge(clSidebarWidthExpanded)
	}
	if flavor == "content" {
		footerClass = clSidebarFooterContent
	}
	if p.Disabled {
		rootClass = rootClass.Merge(clSidebarDisabled)
	}

	sidebarID := strings.TrimSpace(p.ID)
	if sidebarID == "" {
		sidebarID = flavor + "-sidebar"
	}
	if collapsible && !strings.HasSuffix(sidebarID, "-collapsible") {
		sidebarID += "-collapsible"
	}
	label := strings.TrimSpace(p.NavigationLabel)
	if label == "" {
		if flavor == "content" {
			label = "Content navigation"
		} else {
			label = "Admin navigation"
		}
	}

	rootProps := p.ComponentProps
	rootProps.ID = ""
	rootProps.Disabled = false
	attrs := append(baseAttrs(rootProps),
		h.ID(sidebarID),
		classes(rootClass.Compile(), p.Class),
		g.Attr("data-component", "sidebar"),
		g.Attr("data-sidebar-flavor", flavor),
		g.Attr("data-sidebar-collapsible", boolText(collapsible)),
		g.Attr("data-sidebar-collapsed", boolText(p.Collapsed)),
		g.Attr("data-state", sidebarState(p.Collapsed)),
		g.Attr("aria-disabled", boolText(p.Disabled)),
	)
	if collapsible {
		attrs = append(attrs, g.Attr("aria-expanded", boolText(!p.Collapsed)))
	}

	attrs = append(attrs, g.Attr("aria-label", label))

	column := []g.Node{h.Class(columnClass.Compile())}
	if brand := sidebarBrand(p, slots.Brand, flavor, brandClass); brand != nil {
		column = append(column, brand)
	}
	column = append(column, h.Div(
		h.Class(navWrapClass.Compile()),
		h.Nav(
			h.Class(navClass.Compile()),
			g.Attr("aria-label", label),
			sidebarNavigation(p, slots.Items, flavor),
		),
	))
	if len(slots.Footer) > 0 {
		column = append(column, h.Footer(
			h.Class(footerClass.Compile()),
			g.Attr("data-sidebar-footer", ""),
			g.Group(slots.Footer),
		))
	}

	attrs = append(attrs, h.Div(
		h.Class(clSidebarInner.Compile()),
		h.Div(column...),
	))
	return h.Aside(attrs...)
}

func sidebarState(collapsed bool) string {
	if collapsed {
		return "collapsed"
	}
	return "expanded"
}

func sidebarBrand(
	p SidebarProps,
	brand []g.Node,
	flavor string,
	class style.ClassList,
) g.Node {
	if len(brand) > 0 {
		return h.Header(
			h.Class(class.Compile()),
			g.Attr("data-sidebar-brand", ""),
			g.Group(brand),
		)
	}
	label := strings.TrimSpace(p.BrandLabel)
	if label == "" && flavor == "admin" {
		label = "Admin"
	}
	if label == "" {
		return nil
	}
	href := strings.TrimSpace(p.BrandHref)
	if href == "" {
		href = "/admin"
	}
	// The colour is chosen here rather than carried in from the caller: the two columns differ in
	// lightness, and no token is legible on both. See clSidebarBrandLinkAdmin's comment.
	link, text := clSidebarBrandLinkAdmin, clSidebarBrandTextAdmin
	if flavor == "content" {
		link, text = clSidebarBrandLinkContent, clSidebarBrandTextContent
	}
	return h.Header(
		h.Class(class.Compile()),
		g.Attr("data-sidebar-brand", ""),
		h.A(
			h.Href(href),
			h.Class(link.Compile()),
			h.Span(h.Class(text.Compile()), g.Text(label)),
		),
	)
}

func sidebarNavigation(p SidebarProps, richItems []g.Node, flavor string) g.Node {
	if len(richItems) > 0 {
		return g.Group(richItems)
	}
	if len(p.Sections) > 0 {
		sections := make([]g.Node, 0, len(p.Sections))
		for _, section := range p.Sections {
			sections = append(sections, sidebarSectionNode(p, section, flavor))
		}
		return g.Group(sections)
	}
	items := make([]g.Node, 0, len(p.Items))
	for _, item := range p.Items {
		items = append(items, sidebarItemNode(p, item, flavor, 0))
	}
	return h.Ul(h.Class(clSidebarSectionList.Compile()), g.Group(items))
}

func sidebarSectionNode(
	p SidebarProps,
	section SidebarSection,
	flavor string,
) g.Node {
	sectionID := sidebarResolvedID(section.ID, section.Label)
	attrs := []g.Node{
		h.Class(clSidebarSection.Compile()),
		g.Attr("data-sidebar-section", sectionID),
		g.Attr("data-sidebar-search-section", "true"),
	}
	if tone := strings.TrimSpace(section.Tone); tone != "" {
		attrs = append(attrs, g.Attr("data-sidebar-tone", tone))
	}
	if searchText := strings.TrimSpace(section.SearchText); searchText != "" {
		attrs = append(attrs, g.Attr("data-sidebar-search-text", searchText))
	}
	if section.Label != "" || section.Glyph != "" {
		headerClass := clSidebarSectionHeaderAdmin
		if flavor == "content" {
			headerClass = clSidebarSectionHeaderContent
		}
		header := []g.Node{
			h.Class(headerClass.Compile()),
			g.Attr("data-sidebar-section-header", ""),
		}
		if section.Glyph != "" {
			header = append(header, h.Span(
				h.Class(clSidebarSectionGlyph.Compile()),
				g.Attr("data-sidebar-section-glyph", "true"),
				g.Attr("aria-hidden", "true"),
				g.Text(section.Glyph),
			))
		}
		if section.Label != "" {
			header = append(header, h.Span(
				g.Attr("data-sidebar-section-label", "true"),
				g.Text(section.Label),
			))
		}
		attrs = append(attrs, h.Div(header...))
	}
	items := make([]g.Node, 0, len(section.Items))
	for _, item := range section.Items {
		items = append(items, sidebarItemNode(p, item, flavor, 0))
	}
	attrs = append(attrs, h.Ul(h.Class(clSidebarSectionList.Compile()), g.Group(items)))
	return h.Section(attrs...)
}

func sidebarItemNode(
	p SidebarProps,
	item SidebarItem,
	flavor string,
	depth int,
) g.Node {
	active := sidebarItemActive(item, p.Current)
	disabled := p.Disabled || item.Disabled
	itemID := sidebarResolvedID(item.ID, item.Label)
	attrs := []g.Node{
		g.Attr("data-sidebar-item", itemID),
		g.Attr("data-sidebar-depth", itoa(depth)),
		g.Attr("data-active", boolText(active)),
		g.Attr("data-has-badge", boolText(item.Badge != "")),
	}
	if active {
		attrs = append(attrs, g.Attr("data-state", "active"))
	} else {
		attrs = append(attrs, g.Attr("data-state", "idle"))
	}
	if disabled {
		attrs = append(attrs, g.Attr("data-disabled", "true"))
	}
	if searchText := strings.TrimSpace(item.SearchText); searchText != "" {
		attrs = append(attrs,
			g.Attr("data-sidebar-search-item", "true"),
			g.Attr("data-sidebar-search-text", searchText),
		)
	}
	attrs = append(attrs, attrPairs(item.Attrs)...)

	linkClass := clSidebarLinkAdmin
	activeClass := clSidebarLinkActiveAdmin
	idleClass := clSidebarLinkIdleAdmin
	prefixClass := clSidebarPrefixAdmin
	labelClass := clSidebarLabelVisible
	if flavor == "content" {
		linkClass = clSidebarLinkContent
		activeClass = clSidebarLinkActiveContent
		idleClass = clSidebarLinkIdleContent
		prefixClass = clSidebarPrefixContent
		labelClass = clSidebarLabelContent
	}
	if p.Collapsed && flavor == "admin" {
		linkClass = linkClass.Merge(clSidebarLinkPadCollapsed)
		labelClass = clSidebarLabelHidden
	} else {
		linkClass = linkClass.Merge(clSidebarLinkPadExpanded)
	}
	if active {
		linkClass = linkClass.Merge(activeClass)
	} else {
		linkClass = linkClass.Merge(idleClass)
	}
	if disabled {
		linkClass = linkClass.Merge(clSidebarItemDisabled)
	}

	content := []g.Node{h.Class(linkClass.Compile()), g.Attr("data-nav", itemID)}
	if active {
		content = append(content, g.Attr("aria-current", "page"))
	}
	if disabled {
		content = append(content, g.Attr("aria-disabled", "true"))
	}
	if p.Collapsed && flavor == "admin" {
		content = append(content, g.Attr("aria-label", item.Label))
	}
	if validIconName(item.Icon) {
		content = append(content, h.Span(
			g.Attr("data-sidebar-item-icon", ""),
			glyph(item.Icon),
		))
	}
	if item.Prefix != "" && !(p.Collapsed && flavor == "admin") {
		content = append(content, h.Span(
			h.Class(prefixClass.Compile()),
			g.Attr("data-sidebar-item-prefix", "true"),
			g.Text(item.Prefix),
		))
	}
	content = append(content, h.Span(
		h.Class(labelClass.Compile()),
		g.Attr("data-sidebar-item-label", "true"),
		g.Text(item.Label),
	))
	if item.Badge != "" {
		content = append(content, Badge(BadgeProps{
			Label: item.Badge, Variant: item.BadgeVariant, Size: "sm",
		}))
	}
	if len(item.Children) > 0 {
		content = append(content, h.Span(
			g.Attr("data-sidebar-item-chevron", ""),
			glyph("chevron-down"),
		))
	}

	var itemLink g.Node
	if strings.TrimSpace(item.Href) != "" && !disabled {
		itemLink = h.A(append([]g.Node{h.Href(item.Href)}, content...)...)
	} else {
		// A span has no role, and aria-label and aria-disabled are prohibited
		// on an element that has none — axe says so and it is right: an
		// attribute nothing can apply to is an attribute nothing reads. An
		// entry that cannot be followed is still a link, so it says it is one.
		itemLink = h.Span(append([]g.Node{g.Attr("role", "link")}, content...)...)
	}
	if len(item.Children) == 0 {
		attrs = append(attrs, itemLink)
		return h.Li(attrs...)
	}

	children := make([]g.Node, 0, len(item.Children))
	for _, child := range item.Children {
		children = append(children, sidebarItemNode(p, child, flavor, depth+1))
	}
	attrs = append(attrs, h.Div(
		h.Class(clSidebarNestedGroup.Compile()),
		itemLink,
		h.Ul(
			h.Class(clSidebarNestedIndent.Compile()),
			g.Attr("data-sidebar-submenu", itemID),
			g.Group(children),
		),
	))
	return h.Li(attrs...)
}

// namesARoot reports whether href is the whole of an address — one path segment
// with nothing beneath it ("/app", "/ops", "/") — rather than a page under one
// ("/app/task/tasks"). A root begins every address it holds, so prefix-matching
// one marks the entry that leads home as the current page wherever a person
// stands. Which address a root is stays the composition's: nothing here may name
// it, so the shape decides.
func namesARoot(href string) bool {
	return !strings.Contains(strings.Trim(strings.TrimPrefix(href, "/"), "/"), "/")
}

func sidebarItemActive(item SidebarItem, current string) bool {
	if current == "" {
		if item.Active {
			return true
		}
	} else if current == item.Href ||
		(item.Href != "" && !namesARoot(item.Href) && strings.HasPrefix(current, item.Href)) {
		return true
	}
	for _, child := range item.Children {
		if sidebarItemActive(child, current) {
			return true
		}
	}
	return false
}

func sidebarResolvedID(explicit string, fallback string) string {
	if id := strings.TrimSpace(explicit); id != "" {
		return id
	}
	var normalized strings.Builder
	lastDash := false
	for _, char := range strings.ToLower(strings.TrimSpace(fallback)) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			normalized.WriteRune(char)
			lastDash = false
			continue
		}
		if !lastDash && normalized.Len() > 0 {
			normalized.WriteByte('-')
			lastDash = true
		}
	}
	id := strings.Trim(normalized.String(), "-")
	if id == "" {
		return "item"
	}
	return id
}
