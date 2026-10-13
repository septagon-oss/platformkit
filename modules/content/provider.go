package content

import (
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the page store as the resolver sees it, for an application that
// names it in Use: `Use(content.Module)`.
//
// The three things it cannot decide are what resolves the images a body embeds,
// who records that a body reads a file — both the file module's — and where a
// page's committed writes are kept, which is modules/history's. Declaring the
// needs is what makes withdrawing file.Module a refusal naming both instead of a
// page that renders a broken image and forgets which files it showed, and what
// makes a composition with no version store a refusal naming the need rather than
// a page whose writes keep no version. A composition with no file storage
// composes richtext.RejectImages and every image reference is refused on write,
// which is the same answer the empty Deps field gave.
var Module = pkit.NewModule("content", wire,
	pkit.Needs[richtext.Files](),
	pkit.Needs[rest.FileUses](),
	pkit.Needs[httpx.Versions](),
	pkit.Provides[contentcontracts.Service](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	svc, manifest := New(Deps{
		Files:    pkit.Get[richtext.Files](w),
		Uses:     pkit.Get[rest.FileUses](w),
		Versions: pkit.Get[httpx.Versions](w),
	})
	pkit.Put(w, svc)
	return manifest, nil
}
