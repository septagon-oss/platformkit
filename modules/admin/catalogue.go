package admin

import (
	"embed"
	"io/fs"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
)

// catalogues is the admin-authored copy as files, one per locale. The English file
// is the source the Go strings are read against — the loader's source language is
// "en" — and the parity check the composition runs refuses a Portuguese file short
// of a key English carries. A composition that opts into the debugging locale
// derives it from this file (kit/locale/providers/xtext's Pseudo): en-XA is these
// same sentences with their letters accented, which is why a string that never came
// from these files stands out on the page rather than blending in.
//
// This replaced a Go builder of the same four keys in both languages, which is how
// every catalogue in the product started and is exactly what decision 0012 rule 2
// refused: copy nobody could add a language to without a commit in this module.
//
//go:embed messages
var catalogues embed.FS

// Catalogue is this module's copy, ready for the application's one xtext.Load.
func Catalogue() xtext.Source {
	files, err := fs.Sub(catalogues, "messages")
	if err != nil {
		panic("modules/admin: its messages directory is not in the binary: " + err.Error())
	}
	return xtext.Source{FS: files, Name: "modules/admin"}
}
