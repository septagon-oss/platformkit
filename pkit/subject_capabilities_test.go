package pkit_test

import (
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/subject"
	subjectcontracts "github.com/septagon-oss/platformkit/modules/subject/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
	"github.com/septagon-oss/platformkit/pkit"
	"testing"
)

type subjectRetained struct{ usercontracts.RetainedLookup }

func TestSubjectProviderNeedsOnlyRetainedAccountLookup(t *testing.T) {
	parts := pkit.NewModule("parts", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[personaldata.Part](w, personaldata.Part{Module: "parts"})
		return module.Module{Name: "parts"}, nil
	}, pkit.Contributes[personaldata.Part]())
	plan, err := pkit.NewApp("subject").Use(doors, desk, subject.Module, parts,
		identityValue[usercontracts.RetainedLookup]("people", subjectRetained{usertest.NewFake()}),
		identityValue[filecontracts.Service]("files", filetest.NewFake(nil, 1024)),
		identityValue[notificationcontracts.Service]("notices", notificationtest.NewFake()),
		identityValue[jobs.TenantLister]("tenants", tenantcontracts.ActiveTenants{Tenants: tenanttest.NewFake()}),
	).Plan(pkit.Deployment{Environment: pkit.Development})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pkit.Value[usercontracts.Service](plan); ok {
		t.Fatal("subject acquired User Service")
	}
	if svc, ok := pkit.Value[subjectcontracts.Service](plan); !ok || svc == nil {
		t.Fatal("subject was not built")
	}
}
