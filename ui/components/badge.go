package components

// badge.go renders the badge: a tone, a label, and the slots a Go caller
// composes instead of a string.

import (
	"strconv"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// Badge renders BadgeProps without adornment slots.
func Badge(p BadgeProps) g.Node {
	return BadgeWithSlots(p, BadgeSlots{})
}

// BadgeSlots carries trusted Go-composed adornments. Portable delivery graphs
// use the equivalent named iconStart and iconEnd slots.
type BadgeSlots struct {
	IconStart []g.Node
	IconEnd   []g.Node
}

// BadgeWithSlots renders a badge with optional leading and trailing content.
func BadgeWithSlots(p BadgeProps, slots BadgeSlots) g.Node {
	variant := strings.ToLower(strings.TrimSpace(p.Variant))
	if _, exists := clBadgeVariant[variant]; !exists {
		variant = "primary"
	}
	tone := strings.ToLower(strings.TrimSpace(p.Tone))
	if _, exists := clBadgeTone[tone]; !exists {
		tone = "neutral"
	}
	size := strings.ToLower(strings.TrimSpace(p.Size))
	if _, exists := clBadgeSize[size]; !exists {
		size = "md"
	}
	appearance := variantOr(clBadgeVariant, variant, "primary")
	if tone != "neutral" {
		appearance = variantOr(clBadgeTone, tone, "neutral")
	}
	cl := clBadgeBase.
		Merge(appearance).
		Merge(variantOr(clBadgeSize, size, "md"))
	var children []g.Node
	children = append(children, baseAttrs(p.ComponentProps)...)
	children = append(
		children,
		classes(cl.Compile(), p.Class),
		g.Attr("data-component", "badge"),
		g.Attr("data-variant", variant),
		g.Attr("data-tone", tone),
		g.Attr("data-size", size),
	)
	if p.Live {
		children = append(children, h.Role("status"), g.Attr("aria-live", "polite"))
	}
	if p.Dot {
		children = append(children, h.Span(
			h.Class(clBadgeDot.Merge(variantOr(clBadgeDotTone, tone, "neutral")).Compile()),
			g.Attr("aria-hidden", "true"),
			g.Attr("data-badge-dot", "true"),
		))
	}
	children = append(children, slots.IconStart...)
	children = append(children, g.Text(p.Label))
	if p.Count > 0 {
		count := strconv.Itoa(p.Count)
		if p.Count > 99 {
			count = "99+"
		}
		children = append(children, h.Span(
			h.Class(clBadgeCount.Compile()),
			g.Attr("data-badge-count", "true"),
			g.Text(count),
		))
	}
	children = append(children, slots.IconEnd...)
	if p.Removable {
		removeLabel := strings.TrimSpace(p.RemoveLabel)
		if removeLabel == "" {
			removeLabel = "Remove"
			if strings.TrimSpace(p.Label) != "" {
				removeLabel += " " + strings.TrimSpace(p.Label)
			}
		}
		children = append(children, h.Button(
			h.Type("button"),
			h.Class(clBadgeRemove.Compile()),
			g.Attr("aria-label", removeLabel),
			g.Attr("data-badge-remove", "true"),
			Icon(IconProps{Name: "x-mark", Size: "xs"}),
		))
	}
	return h.Span(children...)
}
