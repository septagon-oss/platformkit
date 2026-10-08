package billing

import (
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the plan catalogue as the resolver sees it, for an application that
// names it in Use: `Use(billing.Module)`.
//
// httpx.Entitler is the one contract it hands out — what a tenant's subscription
// includes, asked of this module for every operation whose declaration names a
// feature — and it is the module that answers it, so the answer is put here and
// not by a composition file that happened to hold the value.
//
// FromDeployment names `manual` because that is the only implementation in this
// repository: the ones that speak to a payment processor live outside it. A
// client that composes a processor module beside this one gets 0074's refusal to
// choose, which is where that decision belongs, rather than a field that
// silently overwrote another.
var Module = pkit.NewModule("billing", wire,
	pkit.Needs[jobs.TenantLister](),
	pkit.Provides[httpx.Entitler](),
	pkit.FromDeployment(pkit.Implementation{Name: "manual"}),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	// The picked name is checked rather than trusted: a deployment that names an
	// implementation this module does not declare is refused by the resolver
	// first, so the only way to reach a name that is not manual is a processor
	// module composed beside this one — and that is the ambiguity Choose refuses,
	// not a provider.
	plans, manifest := New(Deps{
		Tenants:  pkit.Get[jobs.TenantLister](w),
		Payments: Manual(),
	})
	pkit.Put[httpx.Entitler](w, plans)
	return manifest, nil
}
