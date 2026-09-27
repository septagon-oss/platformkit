package components

// tabs.go renders the tab list, its panels and which tab is selected.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// TabSlot is one trusted Go tab-panel composition. Portable navigation-only
// tabs remain available through TabsProps.Items; rich panel bodies use this
// slot rather than a second component implementation.
type TabSlot struct {
	ID       string
	Label    string
	Icon     string
	Badge    string
	Disabled bool
	HxGet    string
	Content  []g.Node
}

// TabsSlots carries the ordered tab panels projected into TabsWithSlots.
type TabsSlots struct {
	Tabs []TabSlot
}

type tabsStyle struct {
	root, list, button, active, inactive string
}

// Tabs renders portable navigation tabs. Use TabsWithPanels when each tab owns
// a panel body on the current page.
func Tabs(p TabsProps) g.Node {
	style := resolveTabsStyle(p)
	active := activeItemKey(p.Items, p.ActiveTab)
	if p.Disabled {
		active = ""
	}
	items := []g.Node{
		h.Class(style.list),
		h.Role("tablist"),
		g.Attr("aria-orientation", tabsOrientation(p.Orientation)),
	}
	for _, it := range p.Items {
		disabled := it.Disabled || p.Disabled
		isActive := it.Key == active && !disabled
		stateClass := style.inactive
		if isActive {
			stateClass = style.active
		}
		className := strings.TrimSpace(style.button + " " + stateClass)
		if disabled {
			className += " " + clTabsDisabled.Compile()
		}
		node := []g.Node{
			h.Class(className),
			h.Role("tab"),
			g.Attr("aria-selected", boolText(isActive)),
			g.Attr("aria-disabled", boolText(disabled)),
		}
		if it.Icon != "" {
			node = append(node, h.Span(h.Class(clTabsIcon.Compile()), glyph(it.Icon)))
		}
		node = append(node, g.Text(it.Label))
		if it.Badge != "" {
			node = append(node, h.Span(h.Class(clTabsBadge.Compile()), g.Text(it.Badge)))
		}
		if it.URL != "" && !disabled {
			items = append(items, h.A(append(node, h.Href(it.URL))...))
			continue
		}
		button := append(node, h.Type("button"))
		if disabled {
			button = append(button, h.Disabled())
		}
		items = append(items, h.Button(button...))
	}
	rootProps := p.ComponentProps
	rootProps.Disabled = false
	nav := baseAttrs(rootProps)
	nav = append(nav, classes(style.root, p.Class), g.Attr("data-component", "tabs"))
	nav = append(nav, items...)
	return h.Nav(nav...)
}

// TabsWithPanels is the concise application API for controller-backed tabs.
func TabsWithPanels(p TabsProps, tabs ...TabSlot) g.Node {
	return TabsWithSlots(p, TabsSlots{Tabs: tabs})
}

// TabsWithSlots renders tabs and their panels from one canonical contract.
func TabsWithSlots(p TabsProps, slots TabsSlots) g.Node {
	if len(slots.Tabs) == 0 {
		return nil
	}

	style := resolveTabsStyle(p)
	active := activeSlotID(slots.Tabs, p.ActiveTab)
	if p.Disabled {
		active = ""
	}
	rootID := p.ID
	if rootID == "" {
		rootID = "tabs"
		if active != "" {
			rootID += "-" + active
		}
	}

	tabList := []g.Node{
		h.Class(style.list),
		h.Role("tablist"),
		g.Attr("aria-orientation", tabsOrientation(p.Orientation)),
	}
	panels := []g.Node{h.Class(clTabsPanels.Compile())}
	for _, tab := range slots.Tabs {
		disabled := tab.Disabled || p.Disabled
		isActive := tab.ID == active && !disabled
		panelID := rootID + "-panel-" + tab.ID
		tabID := rootID + "-tab-" + tab.ID
		stateClass := style.inactive
		if isActive {
			stateClass = style.active
		}
		buttonClass := strings.TrimSpace(style.button + " " + stateClass)
		if disabled {
			buttonClass += " " + clTabsDisabled.Compile()
		}
		button := []g.Node{
			h.Class(buttonClass), h.Type("button"), h.Role("tab"), h.ID(tabID),
			g.Attr("aria-controls", panelID),
			g.Attr("aria-selected", boolText(isActive)),
			g.Attr("aria-disabled", boolText(disabled)),
			g.Attr("tabindex", activeTabIndex(isActive)),
			g.Attr("data-tabs-tab", tab.ID),
			g.Attr("data-tabs-active-classes", style.active),
			g.Attr("data-tabs-inactive-classes", style.inactive),
			g.Attr("data-action", "click->tabs#activate"),
		}
		if disabled {
			button = append(button, h.Disabled())
		}
		if tab.Icon != "" {
			button = append(button, h.Span(h.Class(clTabsIcon.Compile()), glyph(tab.Icon)))
		}
		button = append(button, h.Span(g.Text(tab.Label)))
		if tab.Badge != "" {
			button = append(button, h.Span(h.Class(clTabsBadge.Compile()), g.Text(tab.Badge)))
		}
		tabList = append(tabList, h.Button(button...))

		panel := []g.Node{
			h.Class(clTabsPanel.Compile()), h.ID(panelID), h.Role("tabpanel"),
			g.Attr("aria-labelledby", tabID),
			g.Attr("aria-hidden", boolText(!isActive)),
			g.Attr("data-tabs-panel", tab.ID),
			g.Attr("data-state", tabState(isActive)),
		}
		if !isActive {
			panel = append(panel, g.Attr("hidden"))
		}
		hxGet := tab.HxGet
		if hxGet == "" {
			hxGet = p.HxGet
		}
		if hxGet != "" {
			panel = append(panel,
				g.Attr("data-tabs-lazy", "true"),
				g.Attr("hx-get", hxGet),
				g.Attr("hx-trigger", "tabs:activate once"),
				g.Attr("hx-swap", "innerHTML"),
				h.Div(h.Class(clTabsLazy.Compile()),
					h.Span(h.Class(clTabsLazyLabel.Compile()), g.Text(fallbackText(p.LoadingLabel, "Loading...")))),
			)
		} else {
			panel = append(panel, tab.Content...)
		}
		panels = append(panels, h.Div(panel...))
	}

	rootProps := p.ComponentProps
	rootProps.Disabled = false
	root := baseAttrs(rootProps)
	root = append(root,
		classes(style.root, p.Class),
		g.Attr("data-component", "tabs"),
		g.Attr("data-controller", "tabs"),
		g.Attr("data-tabs-contract", "1"),
		g.Attr("data-tabs-active-tab-value", active),
		h.Div(tabList...),
		h.Div(panels...),
	)
	return h.Div(root...)
}

func resolveTabsStyle(p TabsProps) tabsStyle {
	orientation := tabsOrientation(p.Orientation)
	root := clTabsRoot.Merge(clTabsRootHorizontal)
	list := clTabsListBase.Merge(clTabsListHorizontal)
	button := clTabsButtonBase
	if orientation == "vertical" {
		root = clTabsRoot.Merge(clTabsRootVertical)
		list = clTabsListBase.Merge(clTabsListVertical)
	}
	if p.Variant == "pills" {
		button = button.Merge(clTabsButtonPills)
		return tabsStyle{root.Compile(), list.Compile(), button.Compile(), clTabsPillsActive.Compile(), clTabsPillsIdle.Compile()}
	}
	if orientation == "vertical" {
		list = list.Merge(clTabsListUnderlineVertical)
		button = button.Merge(clTabsButtonUnderlineVertical)
	} else {
		list = list.Merge(clTabsListUnderlineHorizontal)
		button = button.Merge(clTabsButtonUnderlineHorizontal)
	}
	return tabsStyle{root.Compile(), list.Compile(), button.Compile(), clTabsUnderlineActive.Compile(), clTabsUnderlineIdle.Compile()}
}

func tabsOrientation(value string) string {
	if value == "vertical" {
		return value
	}
	return "horizontal"
}

func activeItemKey(items []TabItem, requested string) string {
	for _, item := range items {
		if item.Key == requested && !item.Disabled {
			return item.Key
		}
	}
	for _, item := range items {
		if !item.Disabled {
			return item.Key
		}
	}
	return ""
}

func activeSlotID(tabs []TabSlot, requested string) string {
	for _, tab := range tabs {
		if tab.ID == requested && !tab.Disabled {
			return tab.ID
		}
	}
	for _, tab := range tabs {
		if !tab.Disabled {
			return tab.ID
		}
	}
	return ""
}

func activeTabIndex(active bool) string {
	if active {
		return "0"
	}
	return "-1"
}

func tabState(active bool) string {
	if active {
		return "active"
	}
	return "inactive"
}
