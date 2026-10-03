package page

import (
	"embed"
	"io/fs"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
)

// catalogues is this package's copy as files, one per locale (decision 0012 rule
// 2): messages/pt-PT.json holds the sentence a refusal is shown in for each of
// the codes kit/httpx publishes, plus the three verdicts this package writes a
// sentence for itself.
//
// There is an en.json, and it carries only the lines this package writes itself:
// the labels, the parts of a denial and the ask's confirmation. A label is copy, so
// its English lives in the catalogue a translator edits rather than in a string
// literal at the call site (see copy.go). What has no English entry is the
// guard-code keys, and that is the loader's rule rather than an omission: the
// English of a refusal is the guard's own line — `AUTH_DENIED: this operation
// requires task.read`, `LIMIT_EXHAUSTED: … try again in 30 seconds` — which carries
// the permission or the number only the refusal knows, and a static English entry
// would replace a sentence that says something with one that says "you may not".
// fault.go passes that line as the readable fallback, so the source language is
// already fully worded and only the translation needs a file.
//
//go:embed messages
var catalogues embed.FS

// Catalogue is this package's copy, ready for the application's one call to
// xtext.Load. It is a function rather than a variable because the merged catalog
// is composed once per application and this package owns no global.
//
// It claims the `fault.` prefix: the sentence a guard refuses with is the kernel's
// verdict in the caller's language, and a composition that could re-word it could
// soften what a refused write says. A product may still write its own English or
// Portuguese for `screens.*`, `admin.*` or anything of its own, in a later source.
func Catalogue() xtext.Source {
	files, err := fs.Sub(catalogues, "messages")
	if err != nil {
		panic("ui/page: its messages directory is not in the binary: " + err.Error())
	}
	return xtext.Source{FS: files, Name: "ui/page", Owns: []string{"fault."}}
}
