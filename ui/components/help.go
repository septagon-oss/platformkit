package components

// help.go draws the one paragraph of supporting copy a field has: the sentence
// the field's author wrote about the field, and the sentences a form writes about
// the control it chose. The paragraph, its id and the aria-describedby that names
// it belong to each field component; what the two owners said belongs here, so a
// second owner's sentence never has to be assembled out of the first one's.

import (
	"slices"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// hasHelp reports whether a field has any supporting copy to draw: a line of its
// own, or a note about the control. A paragraph described by but never drawn and
// one drawn but never described by are the same bug seen from two ends, so both
// halves of that decision read this one rule.
func hasHelp(text string, notes []string) bool {
	return text != "" || slices.ContainsFunc(notes, func(note string) bool { return note != "" })
}

// helpParagraph draws the supporting paragraph under the attributes its owner
// chose — its id and its class — over the sentences two owners wrote: the field's
// own line, then one span per note about the control. The spans exist because a
// joined sentence is translatable by nobody: the field's documentation and the
// note about the control were written by different hands and land in different
// orders in another language. With no note to set beside it, the field's own line
// stays the plain text node it always was.
func helpParagraph(text string, notes []string, attrs ...g.Node) g.Node {
	kept := make([]string, 0, len(notes))
	for _, note := range notes {
		if note != "" {
			kept = append(kept, note)
		}
	}
	sentences := make([]g.Node, 0, len(kept)+1)
	// A field's own line stands as a text node while it is the only sentence here;
	// once a note joins it, the two have different authors and each keeps its own
	// element.
	switch {
	case text != "" && len(kept) > 0:
		sentences = append(sentences, h.Span(g.Text(text)))
	case text != "":
		sentences = append(sentences, g.Text(text))
	}
	for _, note := range kept {
		sentences = append(sentences, h.Span(g.Text(note)))
	}
	return h.P(append(slices.Clone(attrs), sentences...)...)
}
