package subject

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	subjectcontracts "github.com/septagon-oss/platformkit/modules/subject/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is a person's two doors as the resolver sees them, for an application
// that names it in Use: `Use(subject.Module)`.
//
// This is the line that makes the eight contributions answer to something. Each
// owner says `pkit.Contributes[personaldata.Part]()`, and a contribution nobody
// takes is a claim in a manifest and a dead function in a module: the resolver's own
// explanation prints "no module in platformkit takes one" for exactly that, and for
// eight modules it printed it until this value was composed. `Needs[[]personaldata.Part]`
// is the taking, and it is also the ordering — the resolver builds a consumer after
// every contributor (pkit/declare.go), which is why this module does not say
// After(Everything), which would refuse anything it provides.
//
// It needs the person's own service for one read and no write: an export and an
// erasure both begin with the address and the name, which live in users and nowhere
// else the boundary allows (ADR 0020 §1).
var Module = pkit.NewModule("subject", wire,
	pkit.Needs[[]personaldata.Part](),
	pkit.Needs[usercontracts.Service](),
	pkit.Provides[subjectcontracts.Service](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	svc := NewService(pkit.All[personaldata.Part](w), pkit.Get[usercontracts.Service](w), nil)
	pkit.Put[subjectcontracts.Service](w, svc)
	return New(), nil
}
