package forms

import (
	"embed"
	"io/fs"
)

//go:embed messages
var catalogues embed.FS

// Catalogues supplies the translations of copy owned by portable form controls.
// The application composes them with its other message sources.
func Catalogues() fs.FS {
	files, err := fs.Sub(catalogues, "messages")
	if err != nil {
		panic("ui/forms: its messages directory is not in the binary: " + err.Error())
	}
	return files
}
