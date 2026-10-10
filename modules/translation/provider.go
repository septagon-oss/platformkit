package translation

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	translationcontracts "github.com/septagon-oss/platformkit/modules/translation/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the translation table as the resolver sees it, for an application
// that names it in Use: `Use(translation.Module)`.
//
// One need, and it is the whole of what this module knows about the rest of the
// application: whose records may be said in another language. The application
// writes that name down — this module never goes looking for a table, and the
// day it did is the day "which pages have no Portuguese yet?" becomes a question
// one module asks of another module's rows.
//
// What it hands out is the port a mounted Spec knocks on: `?lang=` reads through
// it, the record's translate door writes through it, and its delete forgets the
// record through it. contracts.Service is that port by another spelling — the
// alias in contracts/ names rest.Translations — so the content module asks for
// the kernel's name and is answered by this one.
//
// No machine translator is composed here: Deps.Translator stays nil unless an
// operator writes a provider down, and with it nil the suggest door refuses
// without writing and no text reaches a reader under a reader's name.
var Module = pkit.NewModule("translation", wire,
	pkit.Needs[rest.TranslationSource](),
	pkit.Provides[translationcontracts.Service](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	svc, manifest := New(Deps{
		Sources: []rest.TranslationSource{pkit.Get[rest.TranslationSource](w)},
	})
	pkit.Put[translationcontracts.Service](w, svc)
	return manifest, nil
}
