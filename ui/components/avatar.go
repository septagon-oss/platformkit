package components

// avatar.go renders the person: the disc, the initials behind a missing
// portrait, and the name beside it — said once, whether or not it is a link.

import (
	"slices"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// Avatar renders AvatarProps: a person as a disc.
//
// Three rules carry it, and each is a decision someone would otherwise re-make
// inside a product:
//
//   - A picture when there is one, initials when there is no picture but there is
//     a name, and the `user` glyph when there is neither. It never invents a
//     person: no generated hue behind a letter, because a colour the renderer made
//     up signifies nothing to the person it stands for, and ui/style/README.md
//     exists to stop exactly that invention.
//   - The disc is named after the person, never after its own contents. Initials
//     are aria-hidden and the name rides on the wrapper, or a screen reader says
//     "J P R" for someone whose profile link should say "Jean-Paul Reyes" — on
//     every row of a thirty-row list.
//   - Decorative hides the whole disc from the reader, for the case where the name
//     is already beside it. An avatar read next to its own name is not twice the
//     information; it is a person hearing the same words again.
//
// Sizes are the three this shelf has and an unknown one falls back to md, as every
// other variant here does. `Href` without any name at all renders a plain disc:
// a link that cannot say who it goes to is a mystery to the one person who cannot
// see where it points, and an unnameable person is not a person to link to.
func Avatar(p AvatarProps) g.Node {
	name := strings.TrimSpace(p.Name)
	label := name
	if p.AriaLabel != "" {
		label = p.AriaLabel
	}

	var inner g.Node
	switch {
	case p.Src != "":
		// The picture goes through Media, so alt text, lazy loading and "the bytes
		// never arrived" are decided once in this package and not again here.
		inner = Media(MediaProps{Src: p.Src, Alt: label, Fit: "cover", Lazy: true})
	case name != "":
		inner = h.Span(h.Class(clAvatarInitials.Compile()), g.Attr("aria-hidden", "true"), g.Text(initials(name)))
	default:
		inner = Icon(IconProps{Name: "user", Size: "md"})
	}

	// A disc that links is the anchor itself rather than a div inside one: Link is
	// a text link and cannot hold a picture, and Card in this file already becomes
	// its own anchor rather than nesting two elements to do the same job.
	disc := clAvatar.Merge(variantOr(clAvatarSize, p.Size, "md"))
	if p.Href != "" {
		disc = disc.Merge(clAvatarLink)
	}
	attrs := []g.Node{classes(disc.Compile(), p.Class)}
	attrs = append(attrs, baseAttrs(p.ComponentProps)...)
	// aria-hidden on the anchor would take the whole link out of the accessibility
	// tree: a keyboard user could reach a person and a reader would announce
	// nothing. The name goes on the anchor and only the contents stay quiet — which
	// they already are, since initials are hidden where they appear.
	// Four exits, and one rule decides between them: the name may be audible from
	// exactly one place, the disc's own label or the text beside it. Everything below
	// follows from refusing to let both speak.
	if p.Href != "" {
		if p.Label != "" {
			// The link is the whole line — disc and name. The name in the text is the
			// anchor's accessible name, so the disc goes quiet inside it, and the tap
			// target is the width of the person rather than a 32-pixel disc.
			quiet := append(slices.Clone(attrs), g.Attr("aria-hidden", "true"))
			return h.A(append(baseAttrs(p.ComponentProps), h.Href(p.Href),
				h.Class(clAvatarLabel.Merge(clAvatarLink).Compile()),
				h.Div(append(quiet, inner)...), h.Span(g.Text(p.Label)))...)
		}
		if label != "" {
			attrs = append(attrs, h.Href(p.Href), g.Attr("aria-label", label))
			return h.A(append(attrs, inner)...)
		}
		// A link with no name to give it is not a link: an unnameable person is not a
		// person to link to, and an anchor that says nothing is what a caller with
		// neither a name nor a label would otherwise get.
	}
	switch {
	case p.Decorative || p.Label != "":
		// A visible name beside the disc, or a caller who says the name is already on
		// screen: either way the disc has nothing to add a reader does not have.
		attrs = append(attrs, g.Attr("aria-hidden", "true"))
	case label != "":
		attrs = append(attrs, g.Attr("role", "img"), g.Attr("aria-label", label))
	}
	return labelled(p, h.Div(append(attrs, inner)...))
}

// labelled puts the visible name beside a disc. It is a function and not another
// branch inside Avatar because the exits need it in different combinations, and a
// component whose labelled form only works in one of them is a component with a
// hole in it.
func labelled(p AvatarProps, disc g.Node) g.Node {
	if p.Label == "" {
		return disc
	}
	return h.Div(h.Class(clAvatarLabel.Compile()), disc, h.Span(g.Text(p.Label)))
}

// initials is the derivation, stated once so every product spells a person the
// same way: the first letter of the first word and of the last word, upper-cased.
// One word yields one letter — a name is not a puzzle to be solved for its middle.
// Letters are taken as runes, so a name in any script yields its own first
// characters instead of being cut in half by byte arithmetic.
func initials(name string) string {
	fields := strings.Fields(name)
	switch len(fields) {
	case 0:
		return ""
	case 1:
		return strings.ToUpper(string([]rune(fields[0])[:1]))
	default:
		first, last := []rune(fields[0]), []rune(fields[len(fields)-1])
		return strings.ToUpper(string([]rune{first[0], last[0]}))
	}
}
