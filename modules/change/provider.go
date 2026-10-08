package change

import (
	"github.com/septagon-oss/platformkit/kit/module"
	changecontracts "github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is change control as the resolver sees it, for an application that
// names it in Use: `Use(change.Module)`.
//
// It owns the object — the proposal, its digest, its state machine, the
// four-eyes rule — and no opinion about which writes need one, because that
// opinion is a fact about an installation. So the subjects arrive as
// contributions, and a composition that contributes none mounts the six
// proposal routes over nothing: an object with no subject is a review queue that
// is always empty, which is why the absence is Explain's note rather than a
// silence.
var Module = pkit.NewModule("change", wire,
	pkit.Needs[[]changecontracts.SubjectBinding](),
	pkit.Provides[changecontracts.Service](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	subjects := pkit.All[changecontracts.SubjectBinding](w)
	svc := NewService(subjects)
	manifest := New(Deps{Service: svc})
	pkit.Put[changecontracts.Service](w, svc)
	return manifest, nil
}
