package export

import (
	"embed"
	"io/fs"
)

// catalogues is this package's copy as files, one per locale: the `gallery.` keys
// are the words a gallery page says around the specimens it frames — the landmark
// around the list of examples, the theme and width menus, the summary over the Go
// snippet, the button that applies a preview. The specimen inside the frame is not
// here: its sentences belong to whoever wrote the example.
//
// The English file repeats what the Go call sites already say. It is not read to
// render an English page — the fallback is — it is the copy a debugging language
// such as en-XA has to be derived from, because a derived locale transforms the
// copy the catalogues hold (see kit/locale/providers/xtext's Pseudo).
//
// Only the translation is a file. The English is the text each renderer passes as
// the readable fallback, so it is worded where it is used and the catalogue is what
// a deployment that answers in more than one language adds.
//
//go:embed messages
var catalogues embed.FS

// Catalogues is this package's copy in the shape xtext.Load reads: the files, at
// their root. It hands over no catalog of its own and keeps no global one, so the
// application composes this with the kernel's and its modules' in one call and the
// merge order stays a line somebody wrote.
//
// It claims no key prefix, as ui/resource claims none: "Apply preview" is the
// gallery's own word for what its button does, and a product that words it
// differently writes the same key into its own catalogue rather than a pull
// request into this layer.
func Catalogues() fs.FS {
	files, err := fs.Sub(catalogues, "messages")
	if err != nil {
		panic("ui/export: its messages directory is not in the binary: " + err.Error())
	}
	return files
}
