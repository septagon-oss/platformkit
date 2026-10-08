package audit

import (
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	contracts "github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the trail as the resolver sees it, for an application that names it
// in Use rather than building it by hand: `Use(audit.Module)`.
//
// It hands out no contract, and that is the module: the trail is read, never
// called. What it cannot decide for itself is who to walk with the retention
// sweep (the tenant module answers) and under which plan feature the routes ask
// (this product answers, and composes no provider when it sells the trail to
// everybody), and how long a row lives, which is a deployment's setting.
var Module = pkit.NewModule("audit", wire,
	pkit.Needs[jobs.TenantLister](),
	pkit.Contributes[personaldata.Part](),
	pkit.Optional[contracts.Plan](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	settings := pkit.Config(w, func(c config.Config) config.Audit { return c.Audit })
	pkit.Put[personaldata.Part](w, internal.PersonalData())
	return New(Deps{
		Tenants:       pkit.Get[jobs.TenantLister](w),
		RetentionDays: settings.RetentionDays,
		Feature:       string(pkit.Get[contracts.Plan](w)),
	}), nil
}
