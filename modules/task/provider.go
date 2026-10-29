package task

import (
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/internal"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the task desk as the resolver sees it, for an application that names
// it in Use: `Use(task.Module)`.
//
// The desk's lifecycle service comes out as a contract for the first time. It
// was never put anywhere: nothing in the reference application takes it, which is
// exactly the case Explain's "no module in platformkit needs it" note exists for
// — a reader is told rather than left in silence (0074 rule 4). A product that
// composes a desk of its own takes it from here rather than rebuilding one.
//
// Four contracts come out of that one service, because a module that needs part
// of a task desk asks for that part: Service for the three lifecycle commands,
// Creator to raise a task, Reader to read one, LockedReader to read the row a diff
// is made against. They are one value under four keys, so no composition can be
// handed a creator that is not the service its own routes write through.
// contracts.Writer is deliberately not among them: Save is the one door that moves
// a protected field with no gate in front of it, and handing it to every
// composition that asked would widen that door by composition rather than by
// decision. A product that composes the change-control subject builds it by name.
//
// Which task a coordinator may resolve is decided by an object-scope policy, and
// a policy is this product's object — a rego file it wrote, embedded, priced in
// its own repository. So it is Optional: a composition that names none has the
// generic desk, with route grants and the tenant floor and nothing above them.
//
// The gate is Optional for the same kind of reason, and it is kit/rest's own type
// because the door belongs to the routes a Spec mounts: this module hands it to its
// own mount rather than writing around them, and which of a task's fields an
// installation will not let one account move alone is no fact this module owns.
var Module = pkit.NewModule("task", wire,
	pkit.Needs[jobs.TenantLister](),
	pkit.Optional[tenancy.Policy](),
	pkit.Optional[rest.Gate](),
	pkit.Provides[taskcontracts.Service](),
	pkit.Provides[taskcontracts.Creator](),
	pkit.Provides[taskcontracts.Reader](),
	pkit.Provides[taskcontracts.LockedReader](),
	pkit.Contributes[personaldata.Part](),
)

// wire builds the desk once and puts it under every contract it answers, so the
// value a consumer reads as Creator is the value the mounted routes create with.
// The concrete service is held rather than the narrowest interface it answers,
// which is what lets one object reach four keys with its identity intact.
func wire(w *pkit.Wiring) (module.Module, error) {
	svc := internal.NewService()
	if policy := pkit.Get[tenancy.Policy](w); policy != nil {
		svc.Policy = policy
	}
	manifest, err := NewChecked(Deps{
		Service: svc,
		Tenants: pkit.Get[jobs.TenantLister](w),
		Gate:    pkit.Get[rest.Gate](w),
	})

	if err != nil {
		return module.Module{}, err
	}
	pkit.Put[taskcontracts.Service](w, svc)
	pkit.Put[taskcontracts.Creator](w, svc)
	pkit.Put[taskcontracts.Reader](w, svc)
	pkit.Put[taskcontracts.LockedReader](w, svc)
	pkit.Put[personaldata.Part](w, internal.PersonalData())
	return manifest, nil
}
