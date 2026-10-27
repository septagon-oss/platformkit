package content

import (
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/content/internal"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the page store as the resolver sees it, for an application that
// names it in Use: `Use(content.Module)`.
//
// The two things it cannot decide are what resolves the images a body embeds and
// who records that a body reads a file — both are the file module's, and declaring
// the needs is what makes withdrawing file.Module a refusal naming both instead of
// a page that renders a broken image and forgets which files it showed. A composition with no file storage composes richtext.RejectImages
// and every image reference is refused on write, which is the same answer the
// empty Deps field gave.
//
// The third is the port its own translatable fields write through. The title and
// the body carry `i18n:"translatable"`, and a Spec that declares them with no port
// to answer `?lang=` is refused at mount — so this need says in the resolver's
// words what that check says at boot: a page store with translatable text is built
// after the module that holds the other languages, and an application that names
// content and no translation is refused by name rather than mounting a record
// screen whose Translate form has nowhere to write.
//
// The fourth is where a page's committed writes are kept, which is
// modules/history's. Declaring it is what makes a composition with no version
// store a refusal naming the need rather than a page whose writes keep no
// version.
var Module = pkit.NewModule("content", wire,
	pkit.Needs[richtext.Files](),
	pkit.Needs[rest.FileUses](),
	pkit.Needs[rest.Translations](),
	pkit.Needs[rest.FileShown](),
	pkit.Needs[httpx.Versions](),
	pkit.Provides[contentcontracts.Service](),
	pkit.Contributes[personaldata.Part](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	svc, manifest, err := NewChecked(Deps{
		Files:        pkit.Get[richtext.Files](w),
		Uses:         pkit.Get[rest.FileUses](w),
		Translations: pkit.Get[rest.Translations](w),
		Shown:        pkit.Get[rest.FileShown](w),
		Versions:     pkit.Get[httpx.Versions](w),
	})

	if err != nil {
		return module.Module{}, err
	}
	pkit.Put(w, svc)
	pkit.Put[personaldata.Part](w, internal.PersonalData())
	return manifest, nil
}
