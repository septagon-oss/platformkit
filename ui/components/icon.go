package components

// icon.go renders a canonical glyph at a named size. The vector comes from
// ui/icon; a name nothing knows renders nothing rather than a broken box.

import (
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/ui/icon"
)

// Icon renders the OSS provider's vector directly into the document. Product
// and client providers extend the glyph vocabulary behind icon.Resolve while
// this atom retains sizing, semantic tone, and accessibility ownership.
func Icon(p IconProps) g.Node {
	size := p.Size
	if size == "" {
		size = "md"
	}
	tone := p.Tone
	if tone == "" {
		tone = "neutral"
	}
	cl := clIcon.
		Merge(variantOr(clIconSize, size, "md")).
		Merge(variantOr(clIconTone, tone, "neutral"))
	glyph, known := icon.Resolve(p.Name)
	nodes := baseAttrs(p.ComponentProps)
	nodes = append(
		nodes,
		classes(cl.Compile(), p.Class),
		g.Attr("xmlns", "http://www.w3.org/2000/svg"),
		g.Attr("viewBox", icon.ViewBox),
		g.Attr("fill", "currentColor"),
		g.Attr("focusable", "false"),
		g.Attr("data-pk-icon", p.Name),
		g.Attr("data-pk-icon-canonical", glyph.Name),
	)
	if !known {
		nodes = append(nodes, g.Attr("data-pk-icon-fallback", "true"))
	}
	if p.AriaLabel == "" {
		nodes = append(nodes, g.Attr("aria-hidden", "true"))
	} else {
		nodes = append(nodes, g.Attr("role", "img"), g.Attr("aria-label", p.AriaLabel))
	}
	nodes = append(nodes, g.Raw(glyph.Body))
	return g.El("svg", nodes...)
}
