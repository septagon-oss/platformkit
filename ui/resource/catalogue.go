package resource

import (
	"embed"
	"io/fs"
)

// catalogues is this package's copy as files, one per locale. The `screens.` keys
// are the words a generated screen says — the count under a list title, the label
// on a create button, the name a pager's landmark carries — so the whole
// vocabulary lives with the package that writes the screens, including the two
// keys ui/screens raises on the forms it mounts (screens.new and
// screens.edit_item), which are the same words at the same addresses.
//
// Only the translation is a file. The English is the text each renderer passes as
// the readable fallback, so it is worded where it is used and the catalogue is
// what a deployment that answers in more than one language adds.
//
// The generated forms are the same surface as the generated lists, and so are the
// sentences kit/rest's form reader refuses them with: those keys are named by the
// reader (`invalid.rest.form.` plus the shape of what went wrong, because no module
// is involved in a value that did not decode) and answered here, because this is the
// only catalogue a generated form is ever drawn from. rest.invalid and
// rest.refusedProblem name where they are raised.
//
//go:embed messages
var catalogues embed.FS

// Catalogues is this package's copy in the shape xtext.Load reads: the files, at
// their root. It hands over no catalog of its own and keeps no global one, so the
// application composes this with the kernel's and its modules' in one call and the
// merge order stays a line somebody wrote.
//
// It claims no key prefix. What a generated screen calls "Delete" is the product's
// to re-word; the sentences the kernel refuses with are not (see ui/page).
func Catalogues() fs.FS {
	files, err := fs.Sub(catalogues, "messages")
	if err != nil {
		panic("ui/resource: its messages directory is not in the binary: " + err.Error())
	}
	return files
}
