package components

// button.go renders the button and its slot form. slotRegion lives here
// because the button is the only family that wraps a caller's nodes in one.

import (
	"slices"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// Button renders ButtonProps without trusted Go-composed slots.
func Button(p ButtonProps) g.Node {
	return ButtonWithSlots(p, ButtonSlots{})
}

// ButtonSlots carries trusted Go-composed content. Portable delivery graphs
// expose iconStart and iconEnd; Content is reserved for direct composition of
// compound controls whose accessible name is supplied through props.
type ButtonSlots struct {
	IconStart []g.Node
	IconEnd   []g.Node
	Content   []g.Node
}

// ButtonWithSlots renders a native button or, when Href is set, an anchor with
// button styling and link semantics. The two modes intentionally share one
// appearance, accessibility, HTMX, and state implementation.
func ButtonWithSlots(p ButtonProps, slots ButtonSlots) g.Node {
	variant := strings.ToLower(strings.TrimSpace(p.Variant))
	if _, exists := clButtonVariant[variant]; !exists {
		variant = "primary"
	}
	appearance := variantOr(clButtonVariant, variant, "primary")
	tone := strings.ToLower(strings.TrimSpace(p.Tone))
	if _, exists := clButtonTone[tone]; !exists {
		tone = "neutral"
	}
	if tone != "neutral" {
		appearance = clButtonTone[tone]
	}
	size := strings.ToLower(strings.TrimSpace(p.Size))
	if _, exists := clButtonSize[size]; !exists {
		size = "md"
	}
	cl := clButtonBase.
		Merge(appearance).
		Merge(clButtonSize[size])
	if p.FullWidth {
		cl = cl.Merge(clButtonFull)
	}
	if p.IconOnly {
		cl = cl.Merge(clButtonIconOnly)
	}
	if p.Href != "" && p.Disabled {
		cl = cl.Merge(clButtonDisabledLink)
	}
	typ := p.Type
	switch typ {
	case "button", "submit", "reset":
	default:
		typ = "button"
	}
	componentProps := p.ComponentProps
	transport := p.HTMXProps
	if p.Href != "" {
		// disabled is not a valid anchor attribute; link mode emits the
		// equivalent accessible state below.
		componentProps.Disabled = false
		if p.Disabled {
			// ARIA and pointer-events alone cannot prevent keyboard activation.
			transport = HTMXProps{Disable: true}
		}
	}
	var children []g.Node
	children = append(children, baseAttrs(componentProps, htmxAttrs(transport)...)...)
	children = append(children,
		classes(cl.Compile(), p.Class),
		g.Attr("data-component", "button"),
		g.Attr("data-variant", variant),
		g.Attr("data-tone", tone),
	)
	if label := p.AriaLabel; label != "" {
		children = append(children, g.Attr("aria-label", label))
	} else if p.IconOnly && p.Label != "" {
		children = append(children, g.Attr("aria-label", p.Label))
	}
	if p.Loading {
		children = append(children,
			g.Attr("data-loading", "true"),
			g.Attr("aria-busy", "true"),
		)
	}
	if len(slots.Content) > 0 {
		children = append(children, slotRegion("Content", slots.Content))
	} else if p.Loading {
		indicatorAppearance := variantOr(clButtonLoadingVariant, variant, "primary")
		if tone != "neutral" {
			indicatorAppearance = variantOr(clButtonLoadingTone, tone, "brand")
		}
		children = append(children, spinnerWithAppearance(
			SpinnerProps{Size: "sm", Label: ""},
			indicatorAppearance,
		))
	} else if len(slots.IconStart) > 0 {
		children = append(children, slotRegion("IconStart", slots.IconStart))
	}
	if len(slots.Content) == 0 && !p.IconOnly {
		children = append(children, g.Raw("<!--pk-text:label-->"), g.Text(p.Label), g.Raw("<!--/pk-text:label-->"))
	}
	if len(slots.Content) == 0 && !p.Loading {
		if len(slots.IconEnd) > 0 {
			children = append(children, slotRegion("IconEnd", slots.IconEnd))
		}
	}
	if p.Href != "" {
		children = append(children, g.Attr("data-button-as-link", "true"))
		if p.Disabled {
			children = append(children,
				h.Role("link"),
				g.Attr("aria-disabled", "true"),
				g.Attr("tabindex", "-1"),
			)
		} else {
			children = append(children, h.Href(p.Href))
		}
		return h.A(children...)
	}
	children = append(children, h.Type(typ))
	return h.Button(children...)
}

// slotRegion retains the source-owned slot name without introducing a DOM box.
func slotRegion(name string, nodes []g.Node) g.Node {
	return g.Group{
		g.Raw("<!--pk-slot:" + name + "-->"),
		g.Group(slices.Clone(nodes)),
		g.Raw("<!--/pk-slot:" + name + "-->"),
	}
}
