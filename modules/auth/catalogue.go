package auth

import (
	"embed"
	"io/fs"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
)

// catalogues is this module's copy as files, one per locale. Every entry is a
// permission's label: the words a refusal is allowed to use when it names what a
// person is missing, so the module that defines the grant owns its name in every
// language and the kernel renders a word it did not invent.
//
// There is no English file: the source text is module.Permission.Label in
// module.go, passed as the readable fallback (decision 0012 rule 2).
//
//go:embed messages
var catalogues embed.FS

// Catalogue is this module's copy, ready for the application's one xtext.Load.
func Catalogue() xtext.Source {
	files, err := fs.Sub(catalogues, "messages")
	if err != nil {
		panic("modules/auth: its messages directory is not in the binary: " + err.Error())
	}
	return xtext.Source{FS: files, Name: "modules/auth"}
}
