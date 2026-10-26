package saved

import (
	"embed"
	"io/fs"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
)

//go:embed messages
var catalogues embed.FS

// Catalogue is this module's copy, ready for the application's one xtext.Load. The
// entries are its two permission labels: the words a refusal may use to name what a
// person is missing, so the module that defines a grant owns its name in every
// language and the kernel never renders a key at a person. There is no English file:
// the source text is each module.Permission.Label, passed as the readable fallback.
func Catalogue() xtext.Source {
	files, err := fs.Sub(catalogues, "messages")
	if err != nil {
		panic("modules/saved: its messages directory is not in the binary: " + err.Error())
	}
	return xtext.Source{FS: files, Name: "modules/saved"}
}
