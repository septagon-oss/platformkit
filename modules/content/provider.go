package content

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/richtext"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the page store as the resolver sees it, for an application that
// names it in Use: `Use(content.Module)`.
//
// The one thing it cannot decide is what resolves the images a body embeds —
// that is the file module's richtext.Files, and declaring the need is what makes
// withdrawing file.Module a refusal naming both instead of a page that renders a
// broken image. A composition with no file storage composes richtext.RejectImages
// and every image reference is refused on write, which is the same answer the
// empty Deps field gave.
var Module = pkit.NewModule("content", wire,
	pkit.Needs[richtext.Files](),
	pkit.Provides[contentcontracts.Service](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	svc, manifest := New(Deps{Files: pkit.Get[richtext.Files](w)})
	pkit.Put(w, svc)
	return manifest, nil
}
