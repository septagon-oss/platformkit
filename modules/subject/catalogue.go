package subject

import (
	"embed"
	"io/fs"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
)

//go:embed messages
var catalogues embed.FS

// Catalogue is this module's copy, ready for the application's one xtext.Load.
//
// Two entries, and both are a permission's label: the words a refusal is allowed to use
// when it names what a person is missing. There is no English file — the source text is
// `module.Permission.Label` in module.go, passed as the readable fallback (decision 0012
// rule 2) — and there is no sentence a person reads in here, because the door's own copy
// (what a notice says, what a page says) is the product's, and the reference application
// writes it beside its own screens.
func Catalogue() xtext.Source {
	files, err := fs.Sub(catalogues, "messages")
	if err != nil {
		panic("modules/subject: its messages directory is not in the binary: " + err.Error())
	}
	return xtext.Source{FS: files, Name: "modules/subject"}
}
