package admin

import (
	"embed"
	"io/fs"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
)

// catalogues is the sign-in page's copy as files, one per locale. Four keys, and
// only the translation: the English of this page is the sentence each control
// passes as its readable fallback in internal/pages.go, so putting it in a second
// place would give the page two sources of the same words and a translator one
// more file to keep in step.
//
// This replaced a Go builder of the same four keys in both languages, which is how
// every catalogue in the product started and is exactly what decision 0012 rule 2
// refused: copy nobody could add a language to without a commit in this module.
//
//go:embed messages
var catalogues embed.FS

// Catalogue is this module's copy, ready for the application's one xtext.Load. The
// sign-in page is the only screen that speaks another language today, and the
// module composes nothing: an application that has more copy of its own passes it
// as a later source and the merge order decides, not this file.
func Catalogue() xtext.Source {
	files, err := fs.Sub(catalogues, "messages")
	if err != nil {
		panic("modules/admin: its messages directory is not in the binary: " + err.Error())
	}
	return xtext.Source{FS: files, Name: "modules/admin"}
}
