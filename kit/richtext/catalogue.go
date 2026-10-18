package richtext

import (
	"embed"
	"io/fs"
)

//go:embed messages
var catalogues embed.FS

// Catalogues supplies the copy the refusals of this package speak. The package
// names each refusal with a key (Issue.Key) and holds no sentence of its own for
// a screen: the sentences live here, and the application composes them with its
// other message sources the way it composes ui/forms and ui/resource.
func Catalogues() fs.FS {
	files, err := fs.Sub(catalogues, "messages")
	if err != nil {
		panic("richtext: its messages directory is not in the binary: " + err.Error())
	}
	return files
}
