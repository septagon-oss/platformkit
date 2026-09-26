package components

// media.go renders a picture and what stands in for one that is absent,
// refused or failed.

import (
	"strconv"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/style"
)

// Media renders one picture and, more usefully, what to say while there is not
// one. It exists because Card had an <img> and nothing else: a cut-out still
// being cut, a print job that failed, and a picture somebody may not look at all
// arrived as the same broken-image glyph, and the difference between those three
// is the entire news.
//
// Empty is delegated to EmptyState — it is that state, and the shelf already has
// a voice. Failed and refused get a panel of their own, because an error that
// renders as an empty shelf loses the one thing the caller needs to hear, and a
// permission hidden behind "nothing here" teaches everybody that this product
// lies. The words are the caller's either way: the renderer keeps none, since
// "the cut-out failed" and "the shop is closed" are one state and two sentences.
//
// A status this package does not know renders as failed, under its own name, with
// the caller's reason. Guessing that unknown means "ready" would open a box
// waiting for bytes that are never coming — the defect a caller discovers last.
func Media(p MediaProps) g.Node {
	status := p.Status
	if status == "" {
		status = MediaReady
	}
	switch status {
	case MediaLoading:
		name := p.Alt
		if name == "" {
			name = p.Caption
		}
		if name == "" {
			name = "Loading"
		}
		nodes := append(baseAttrs(p.ComponentProps),
			classes(clCardImageVertical.Merge(clSkeleton).Compile(), p.Class),
			g.Attr("role", "img"), g.Attr("aria-busy", "true"), g.Attr("aria-label", name))
		return h.Div(nodes...)
	case MediaEmpty:
		return EmptyState(EmptyStateProps{
			ComponentProps: p.ComponentProps,
			Title:          p.Reason,
			Compact:        true,
			Bordered:       true,
		})
	case MediaFailed, MediaRefused:
		return mediaAbsent(p, status)
	case MediaReady:
		// The picture itself, below.
	default:
		return mediaAbsent(p, status)
	}
	attrs := []g.Node{g.Attr("src", p.Src), g.Attr("alt", p.Alt)}
	if p.Width > 0 && p.Height > 0 {
		attrs = append(attrs, g.Attr("width", strconv.Itoa(p.Width)), g.Attr("height", strconv.Itoa(p.Height)))
	}
	if p.Lazy {
		attrs = append(attrs, g.Attr("loading", "lazy"))
	}
	// One picture, one class. Horizontal is Card's thumbnail form, and keeping the
	// choice here is what lets cardImage delegate instead of maintaining a second
	// copy of this element where a fix has to be made twice.
	imageClass := clCardImageVertical
	if p.Horizontal {
		imageClass = clCardImageHorizontal
	}
	// Fit is the one piece of layout the caller owns, because it is a claim about
	// the picture and not about the box: "the whole subject matters" is contain,
	// "fill this shape and crop what hangs over" is cover. An image in a fixed
	// round box hangs over the edge without it, which is what an Avatar needs —
	// the prop had been declared and silently ignored until then.
	// Exactly one crop, and a name for the default: Card has cropped its images
	// since it was written, so the empty string means cover rather than nothing.
	switch p.Fit {
	case "contain":
		imageClass = imageClass.Merge(clImageContain)
	default:
		imageClass = imageClass.Merge(clImageCover)
	}

	img := h.Img(append(attrs, h.Class(imageClass.Compile()))...)
	if p.Caption == "" {
		return img
	}
	// A caption outside the picture is what makes it a caption: text inside an
	// alt is read once and cannot be returned to, and a figure lets a reader
	// navigate to it as the pair it is.
	nodes := append(baseAttrs(p.ComponentProps),
		classes(clMediaFigure.Compile(), p.Class),
		img,
		h.FigCaption(h.Class(clMediaCaption.Compile()), g.Raw("<!--pk-text:caption-->"), g.Text(p.Caption), g.Raw("<!--/pk-text:caption-->")))
	return h.Figure(nodes...)
}

// mediaAbsent renders the states where there is no picture to show. Failed and
// refused are not empty and never borrow the empty shelf's markup: the
// difference in border and ground is what a stranger reads as "ask for access"
// versus "look elsewhere".
func mediaAbsent(p MediaProps, status MediaStatus) g.Node {
	nodes := append(baseAttrs(p.ComponentProps),
		classes(clMediaAbsent.Merge(clMediaPanel(status)).Compile(), p.Class),
		g.Attr("data-state", string(status)), g.Attr("role", mediaRole(status)))
	if p.Reason != "" {
		nodes = append(nodes, h.P(h.Class(clEmptyDesc.Compile()),
			g.Raw("<!--pk-text:reason-->"), g.Text(p.Reason), g.Raw("<!--/pk-text:reason-->")))
	}
	return h.Div(nodes...)
}

// mediaRole picks how loudly a state is announced. A failure the caller asked
// for interrupts, because sitting on it is worse; a refusal does not, because the
// page is not wrong and a reader who did not ask for that picture has enough on
// their plate.
func mediaRole(status MediaStatus) string {
	if status == MediaRefused {
		return "note"
	}
	return "alert"
}

// clMediaPanel picks the tone, kept beside mediaRole so a new state cannot arrive
// wearing the other one's border.
func clMediaPanel(status MediaStatus) style.ClassList {
	if status == MediaRefused {
		return clMediaRefused
	}
	return clMediaFailed
}
