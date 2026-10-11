package task

import (
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
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
// Which task a coordinator may resolve is decided by an object-scope policy, and
// a policy is this product's object — a rego file it wrote, embedded, priced in
// its own repository. So it is Optional: a composition that names none has the
// generic desk, with route grants and the tenant floor and nothing above them.
var Module = pkit.NewModule("task", wire,
	pkit.Needs[jobs.TenantLister](),
	pkit.Optional[tenancy.Policy](),
	pkit.Provides[taskcontracts.Service](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	svc := NewService()
	if policy := pkit.Get[tenancy.Policy](w); policy != nil {
		svc = NewServiceWithPolicy(policy)
	}
	manifest := New(Deps{
		Service: svc,
		Tenants: pkit.Get[jobs.TenantLister](w),
	})
	pkit.Put[taskcontracts.Service](w, svc)
	return manifest, nil
}
