package saved

import (
	"embed"
	"io/fs"
)

//go:embed messages
var copy embed.FS

// Catalogues is this page's copy in the shape xtext.Load reads: the files, at their
// root. The English of every key is the readable fallback the renderer passes at the
// call site, so this hands over translations only, and the application decides where
// in the merge order they sit — the same shape ui/resource's catalogues take.
//
// The keys are this grid's own sentences: its pager, its empty states and the two
// words that say a source behind a copy is gone or unreachable. A retained copy's
// own words are not here: they are the values the person kept, read from the row.
func Catalogues() fs.FS {
	files, err := fs.Sub(copy, "messages")
	if err != nil {
		panic("ui/saved: its messages directory is not in the binary: " + err.Error())
	}
	return files
}
