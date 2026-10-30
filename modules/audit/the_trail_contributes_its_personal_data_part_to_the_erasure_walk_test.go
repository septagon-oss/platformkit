package audit_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/pkit"
)

// probeInputs answers the three ports no composition plans without and the tenant
// lister audit.Module Needs, in the shape the task desk's fixture writes.
var probeInputs = pkit.NewModule("probeinputs", func(w *pkit.Wiring) (module.Module, error) {
	pkit.Put[httpx.TenantLoader](w, nil)
	pkit.Put[httpx.Authorizer](w, nil)
	pkit.Put[pkit.Authenticator](w, func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
		return tenancy.Principal{}, true, nil
	})
	pkit.Put[jobs.TenantLister](w, nil)
	return module.Module{Name: "probeinputs"}, nil
}, pkit.Provides[httpx.TenantLoader](), pkit.Provides[httpx.Authorizer](),
	pkit.Provides[pkit.Authenticator](), pkit.Provides[jobs.TenantLister]())

// probeTaker is the erasure orchestrator's read: every module's personal-data claim
// the plan collected. It refuses its own build when the trail's claim is absent, so
// the composition answers rather than this file's query.
func probeTaker() *pkit.Module {
	return pkit.NewModule("probetaker", func(w *pkit.Wiring) (module.Module, error) {
		for _, part := range pkit.All[personaldata.Part](w) {
			if part.Module == "audit" && part.Data != nil {
				return module.Module{Name: "probetaker"}, nil
			}
		}
		return module.Module{}, errors.New("no personal-data part for audit reached the erasure walk")
	}, pkit.Needs[[]personaldata.Part]())
}

// TestTheTrailContributesItsPartToTheErasureWalk asks what the audit module cannot
// answer from inside itself: does the personal-data claim it contributes reach the
// module that composes them? A contribution that stopped arriving — moved, renamed,
// or put after a build that refused — would leave a person's audit rows outside every
// export and erasure, and nothing in this module's own tests would notice.
func TestTheTrailContributesItsPartToTheErasureWalk(t *testing.T) {
	if _, err := pkit.NewApp("probe").Use(probeInputs, audit.Module, probeTaker()).
		Plan(pkit.Deployment{Environment: pkit.Development}); err != nil {
		t.Fatalf("the trail's part: %v", err)
	}
}
