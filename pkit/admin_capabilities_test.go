package pkit_test

import (
	"github.com/septagon-oss/platformkit/modules/admin"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
	"github.com/septagon-oss/platformkit/pkit"
	"testing"
)

type adminRoles struct{ authcontracts.RoleEditor }
type adminSessions struct {
	authcontracts.SessionAdministration
}
type adminTenants struct{ tenantcontracts.Lister }
type adminLookup struct{ usercontracts.Lookup }

func TestAdminProviderNeedsOnlyItsNamedCapabilities(t *testing.T) {
	for _, withPeople := range []bool{false, true} {
		roles := authtest.NewFake(usertest.NewFake())
		app := pkit.NewApp("shell").Use(doors, admin.Module,
			identityValue[authcontracts.RoleEditor]("roles", adminRoles{roles}),
			identityValue[authcontracts.SessionAdministration]("sessions", adminSessions{roles}),
			identityValue[tenantcontracts.Lister]("tenants", adminTenants{tenanttest.NewFake()}))
		if withPeople {
			app.Use(identityValue[usercontracts.Lookup]("people", adminLookup{usertest.NewFake()}))
		}
		plan, err := app.Plan(pkit.Deployment{Environment: pkit.Development})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := pkit.Value[authcontracts.Auth](plan); ok {
			t.Fatal("shell acquired Auth")
		}
		if _, ok := pkit.Value[usercontracts.Service](plan); ok {
			t.Fatal("shell acquired User Service")
		}
		if _, ok := pkit.Value[tenantcontracts.Service](plan); ok {
			t.Fatal("shell acquired Tenant Service")
		}
	}
}
