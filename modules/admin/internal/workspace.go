package internal

import (
	"cmp"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/icon"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// workspaceSections groups only entries Navigation.Visible already admitted.
// Resource declarations own their grouping and icon. Hand-written shell pages
// use their composed addresses; no business module or route prefix is guessed.
func workspaceSections(a addresses, visible []module.NavEntry, resources []httpx.Resource, o resource.Options) []components.SidebarSection {
	var sections []components.SidebarSection
	for _, entry := range visible {
		key, label, glyph := "workspace", o.Text("admin.workspace.workspace", "Workspace"), "file-text"
		for _, r := range resources {
			if r.Screen != entry.Screen {
				continue
			}
			reading := o.Words(r.Schema).Entry(r.Present)
			glyph = cmp.Or(reading.Icon, glyph)
			if reading.Group != nil {
				key, label = reading.Group.Key, reading.Group.Label
			}
			break
		}
		switch entry.Screen {
		case a.roles.at:
			key, label, glyph = "people", o.Text("admin.workspace.people", "People"), "gear"
		case a.tenants.at:
			key, label, glyph = "platform", o.Text("admin.workspace.platform_operations", "Platform operations"), "user"
		}
		if _, known := icon.Resolve(glyph); !known {
			glyph = "file-text"
		}
		i := slices.IndexFunc(sections, func(s components.SidebarSection) bool { return s.ID == key })
		if i < 0 {
			sections = append(sections, components.SidebarSection{ID: key, Label: label})
			i = len(sections) - 1
		}
		sections[i].Items = append(sections[i].Items, components.SidebarItem{Label: entry.Label, Href: entry.Screen, Icon: glyph})
	}
	// Cross-tenant work comes first for operators; Visible has already kept it
	// out of customer navigation. Preserve module order within other groups.
	if i := slices.IndexFunc(sections, func(s components.SidebarSection) bool { return s.ID == "platform" }); i > 0 {
		platform := sections[i]
		copy(sections[1:i+1], sections[:i])
		sections[0] = platform
	}
	return sections
}

func workspacePurpose(a addresses, item components.SidebarItem, resources []httpx.Resource, o resource.Options) string {
	switch item.Href {
	case a.roles.at:
		return o.Text("admin.workspace.roles_purpose", "Review permissions and decide who can do what.")
	case a.tenants.at:
		return o.Text("admin.workspace.tenants_purpose", "Review tenant status and open the workspace you need.")
	}
	for _, r := range resources {
		if r.Screen == item.Href && r.Present.Description != "" {
			return o.Words(r.Schema).Entry(r.Present).Description
		}
	}
	return o.Text("admin.workspace.open", "Open %s in this workspace.", strings.ToLower(item.Label))
}
