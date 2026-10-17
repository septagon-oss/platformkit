package picture

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/picture/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module requires finite templates and a source that authorizes every read.
// Invalid declarations fail dry composition before external resources open.
var Module = pkit.NewModule("picture", wire, pkit.Needs[contracts.Source](), pkit.Needs[contracts.Templates]())

func wire(w *pkit.Wiring) (module.Module, error) {
	return manifest(pkit.Get[contracts.Source](w), pkit.Get[contracts.Templates](w))
}
