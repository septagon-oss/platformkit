package audit

import (
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	contracts "github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the trail as the resolver sees it, for an application that names it
// in Use rather than building it by hand: `Use(audit.Module)`.
//
// It hands out no contract, and that is the module: the trail is read, never
// called. What it cannot decide for itself is who to walk with the retention
// sweep (the tenant module answers) and under which plan feature the routes ask
// (this product answers, and composes no provider when it sells the trail to
// everybody), and how long a row lives and which connection may expire it, which
// are a deployment's settings.
var Module = pkit.NewModule("audit", wire,
	pkit.Needs[jobs.TenantLister](),
	pkit.Optional[contracts.Plan](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	settings := pkit.Config(w, func(c config.Config) config.Audit { return c.Audit })
	store := pkit.Config(w, func(c config.Config) config.Database { return c.Database })
	return New(Deps{
		Tenants:       pkit.Get[jobs.TenantLister](w),
		RetentionDays: settings.RetentionDays,
		RetainURL:     store.RetainURL,
		Feature:       string(pkit.Get[contracts.Plan](w)),
	}), nil
}
