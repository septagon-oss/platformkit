package components

// card.go renders the card: its sections, its padding, and the picture it
// delegates to Media.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// CardSlots is the trusted Go composition seam for the three structural card
// regions. Portable title/description/media data remains in CardProps.
type CardSlots struct {
	Header  []g.Node
	Content []g.Node
	Footer  []g.Node
}

// Card renders free-form card children. Call CardWithSlots when header,
// content, and footer need the canonical section treatment.
func Card(p CardProps, children ...g.Node) g.Node {
	return cardNode(p, CardSlots{}, children)
}

// CardWithSlots renders canonical header/content/footer regions without a
// downstream wrapper or style implementation.
func CardWithSlots(p CardProps, slots CardSlots) g.Node {
	return cardNode(p, slots, nil)
}

func cardNode(p CardProps, slots CardSlots, children []g.Node) g.Node {
	sectioned := len(slots.Header)+len(slots.Content)+len(slots.Footer) > 0
	rootClass := cardRootClasses(p, sectioned)
	nodes := baseAttrs(p.ComponentProps, htmxAttrs(p.HTMXProps)...)
	nodes = append(nodes,
		classes(rootClass, p.Class),
		g.Attr("data-component", "card"),
	)

	body := make([]g.Node, 0, 5)
	if p.Image != "" && normalizedCardImagePosition(p.ImagePosition) == "top" {
		body = append(body, cardImage(p, false))
	}
	if sectioned {
		padding := cardPaddingClasses(p.Padding, true)
		header := slots.Header
		if len(header) == 0 && (p.Title != "" || p.Description != "") {
			header = cardTextHeader(p)
		}
		if len(header) > 0 {
			body = append(body, h.Div(
				h.Class(strings.TrimSpace(padding+" "+clCardHeader.Compile())),
				g.Group(header),
			))
		}
		if len(slots.Content) > 0 {
			body = append(body, h.Div(h.Class(padding), g.Group(slots.Content)))
		}
		if len(slots.Footer) > 0 {
			body = append(body, h.Div(
				h.Class(strings.TrimSpace(padding+" "+clCardFooter.Compile())),
				g.Group(slots.Footer),
			))
		}
	} else {
		body = append(body, cardTextHeader(p)...)
		body = append(body, children...)
	}
	if p.Image != "" && normalizedCardImagePosition(p.ImagePosition) == "bottom" {
		body = append(body, cardImage(p, false))
	}

	position := normalizedCardImagePosition(p.ImagePosition)
	if p.Image != "" && (position == "left" || position == "right") {
		content := h.Div(h.Class(clCardVertical.Compile()), g.Group(body))
		image := cardImage(p, true)
		if position == "left" {
			body = []g.Node{h.Div(h.Class(clCardHorizontal.Compile()), image, content)}
		} else {
			body = []g.Node{h.Div(h.Class(clCardHorizontal.Compile()), content, image)}
		}
	}

	nodes = append(nodes, body...)
	if p.Clickable && p.Href != "" {
		return h.A(append(nodes, h.Href(p.Href))...)
	}
	return h.Article(nodes...)
}

func cardTextHeader(p CardProps) []g.Node {
	var nodes []g.Node
	if p.Title != "" {
		nodes = append(nodes, h.P(h.Class(clCardTitle.Compile()), g.Raw("<!--pk-text:title-->"), g.Text(p.Title), g.Raw("<!--/pk-text:title-->")))
	}
	if p.Description != "" {
		nodes = append(nodes, h.P(h.Class(clCardDesc.Compile()), g.Raw("<!--pk-text:description-->"), g.Text(p.Description), g.Raw("<!--/pk-text:description-->")))
	}
	return nodes
}

// cardImage defers to Media so that a picture has one renderer in this package.
// The output is byte-identical to what this function wrote before Media existed
// — attribute order included — because the export digest and every product page
// built on Card are sensitive to that, and TestMediaDoesNotMoveWhatCardAlready
// Rendered is what keeps the claim honest rather than aspirational.
func cardImage(p CardProps, horizontal bool) g.Node {
	return Media(MediaProps{Src: p.Image, Alt: p.ImageAlt, Horizontal: horizontal})
}

func normalizedCardImagePosition(position string) string {
	switch strings.ToLower(strings.TrimSpace(position)) {
	case "bottom", "left", "right":
		return strings.ToLower(strings.TrimSpace(position))
	default:
		return "top"
	}
}

func cardPaddingClasses(padding string, sectioned bool) string {
	switch strings.ToLower(strings.TrimSpace(padding)) {
	case "none":
		return clCardPadNone.Compile()
	case "small":
		return clCardPadSmall.Compile()
	case "large":
		return clCardPadLarge.Compile()
	case "medium":
		return clCardPadMedium.Compile()
	default:
		if sectioned {
			return clCardPadMedium.Compile()
		}
		return clCardPadDefault.Compile()
	}
}

func cardRootClasses(p CardProps, sectioned bool) string {
	cl := clCardFrame
	if sectioned {
		cl = cl.Merge(clCardSectioned)
	} else {
		switch strings.ToLower(strings.TrimSpace(p.Padding)) {
		case "none":
			cl = cl.Merge(clCardPadNone)
		case "small":
			cl = cl.Merge(clCardPadSmall)
		case "medium":
			cl = cl.Merge(clCardPadMedium)
		case "large":
			cl = cl.Merge(clCardPadLarge)
		default:
			cl = cl.Merge(clCardPadDefault)
		}
	}

	variant := strings.ToLower(strings.TrimSpace(p.Variant))
	if variant != "elevated" && variant != "plain" {
		cl = cl.Merge(clCardBorder)
	}
	shadow := strings.ToLower(strings.TrimSpace(p.Shadow))
	if shadow == "" {
		switch variant {
		case "outlined", "plain":
			shadow = "none"
		case "elevated":
			shadow = "medium"
		default:
			shadow = "small"
		}
	}
	switch shadow {
	case "medium":
		cl = cl.Merge(clCardShadowMedium)
	case "large":
		cl = cl.Merge(clCardShadowLarge)
	case "none":
	default:
		cl = cl.Merge(clCardShadowSmall)
	}
	if p.Hoverable {
		cl = cl.Merge(clCardHoverable)
	}
	if p.Clickable {
		cl = cl.Merge(clCardClickable)
	}
	return cl.Compile()
}
