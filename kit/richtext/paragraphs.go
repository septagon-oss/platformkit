package richtext

import (
	"strings"
)

// Paragraphs are the top-level blocks of the canonical serialisation, in order:
// one entry per paragraph, heading, list, blockquote, figure, table, and one
// entry per fenced code block as a whole.
//
// It exists for the translation module's stale diff, and the reason it is here
// rather than a blank-line split in the module that needs it is that a GFM table
// and a fenced code block both contain lines that are blank in the source and
// are not paragraph breaks. A split that mis-reads one reports a changed
// document where nothing changed, and the reviewer's confidence in every other
// highlight goes with it. So the same AST the renderer walks decides where a
// block begins and ends, over the canonical text the hash is taken of — which
// means two spellings of one document agree about both the digest and the
// paragraphs, and the diff can only ever light up on a difference the hash
// already noticed.
//
// An empty or whitespace-only document has no paragraphs at all, including no
// empty one: "nothing" and "a paragraph of nothing" are the same text and must
// diff the same way.
func Paragraphs(source string) ([]string, error) {
	normal, err := Normalise(source)
	if err != nil {
		return nil, err
	}
	doc, err := Parse(normal)
	if err != nil {
		return nil, err
	}
	// Top-level blocks partition the canonical text: a block runs to where the
	// next one begins. Pos() is on the node interface; End() is not, and the
	// partition is both cheaper and truer than an assertion — the last block
	// takes the remainder of the document, which is exactly what it holds.
	var bounds []int
	for block := doc.root.FirstChild(); block != nil; block = block.NextSibling() {
		bounds = append(bounds, block.Pos())
	}
	var out []string
	for i, start := range bounds {
		stop := len(normal)
		if i+1 < len(bounds) {
			stop = bounds[i+1]
		}
		if start < 0 || start > stop {
			continue
		}
		// The block's own bytes of the canonical text — not its rendered form
		// and not a source the caller may no longer have: the canonical form is
		// what SourceHash digests, so a paragraph the diff calls the same is a
		// paragraph the digest calls the same.
		text := strings.TrimSpace(normal[start:stop])
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return out, nil
}
