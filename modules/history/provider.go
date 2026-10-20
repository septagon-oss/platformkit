package history

import (
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/rest"
	historycontracts "github.com/septagon-oss/platformkit/modules/history/contracts"
	"github.com/septagon-oss/platformkit/modules/history/internal"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the version store as the resolver sees it, for an application that
// names it in Use: `Use(history.Module)`.
//
// Two needs, both because another module owns the answer. rest.FileUses is the file
// ledger: a retained version holds the pictures its body shows, so a version cannot
// be kept honest about what may be deleted without it — a history that stores bodies
// holding images and files no use row is a lie about what retention may delete.
// jobs.TenantLister is how the sweep finds the tenants it retains versions for.
//
// Two provides: the store itself, and httpx.Versions, the port a resource mounted
// with History records through. A resource asks for that one and never imports this
// module; the composition is where the two meet.
var Module = pkit.NewModule("history", wire,
	pkit.Needs[rest.FileUses](),
	pkit.Needs[jobs.TenantLister](),
	pkit.Provides[historycontracts.Service](),
	pkit.Provides[httpx.Versions](),
	pkit.Contributes[personaldata.Part](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	svc, versions, manifest, err := New(Deps{
		Uses:    pkit.Get[rest.FileUses](w),
		Tenants: pkit.Get[jobs.TenantLister](w),
	})
	if err != nil {
		return module.Module{}, err
	}
	pkit.Put(w, svc)
	pkit.Put[httpx.Versions](w, versions)
	pkit.Put[personaldata.Part](w, internal.PersonalData())
	return manifest, nil
}
