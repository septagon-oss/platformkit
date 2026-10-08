package main

import (
	"context"
	"strings"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/module"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	scim "github.com/septagon-oss/platformkit/modules/scim"
	scimcontracts "github.com/septagon-oss/platformkit/modules/scim/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// The identity-provider door, wired.
//
// The scim module names two things it cannot make for itself: the person, which
// is modules/user's service, and the answer to "does this tenant know that role",
// which is this module's own port — role definitions are auth's table, and a
// provisioning module that read them would be two modules' rows in one query. So
// the composition answers it, over the one auth method that is a plain read of
// this tenant's role names.
var scimProvisioning = pkit.NewModule("scim", wireScim,
	pkit.Needs[usercontracts.Service](),
	pkit.Needs[authcontracts.Auth](),
	pkit.Provides[scimcontracts.Service](),
)

func wireScim(w *pkit.Wiring) (module.Module, error) {
	svc, manifest := scim.New(scim.Deps{
		Users: pkit.Get[usercontracts.Service](w),
		Roles: roleNames{auth: pkit.Get[authcontracts.Auth](w)},
	})
	pkit.Put[scimcontracts.Service](w, svc)
	return manifest, nil
}

// roleNames is the adapter between the scim module's question and auth's
// table. It reads rather than caches, because a role can be created in the middle
// of the tenant's life and a mapping bound to a name that stops existing — or
// starts existing — has to be refused on the day that is true, not on the day
// somebody restarted a process.
//
// An error is returned, not swallowed as "no such role": the write is refused on
// an outage, and a mapping that silently bound to nothing is the failure this
// adapter exists to make impossible.
type roleNames struct{ auth authcontracts.Auth }

func (d roleNames) HasRole(ctx context.Context, tx db.Tx[db.Tenant], name string) (bool, error) {
	roles, err := d.auth.Roles(ctx, tx)
	if err != nil {
		return false, err
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for _, r := range roles {
		if strings.EqualFold(r.Name, want) {
			return true, nil
		}
	}
	return false, nil
}
