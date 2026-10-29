package auth

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// GrantsDeps has no configuration: grants read the caller's transaction and principal.
type GrantsDeps struct{}

// NewGrants constructs one stateless role policy and its manifest without opening
// resources. Auth's migration source must be applied before role-backed calls.
func NewGrants(GrantsDeps) (contracts.GrantAuthorizer, contracts.PermissionReader, module.Module) {
	reader := &internal.GrantReader{}
	return reader, reader, module.Module{Name: "authgrants"}
}

// Grants provides role policy independently of account and session construction.
var Grants = pkit.NewModule("authgrants", wireGrants,
	pkit.Provides[contracts.GrantAuthorizer](),
	pkit.Provides[contracts.PermissionReader](),
	pkit.Provides[usercontracts.Granting](),
	pkit.Provides[usercontracts.Administration](),
	pkit.Provides[usercontracts.InvitationTiming](),
	pkit.Provides[usercontracts.InvitationAuthority](),
)

func wireGrants(w *pkit.Wiring) (module.Module, error) {
	authorize, permissions, manifest := NewGrants(GrantsDeps{})
	pkit.Put(w, authorize)
	pkit.Put(w, permissions)
	pkit.Put[usercontracts.Granting](w, roleGranting{authorize: authorize})
	pkit.Put[usercontracts.Administration](w, &usercontracts.AdministrationFunc{Ask: internal.AdministeringRoles})
	pkit.Put(w, usercontracts.InvitationTiming{Lifetime: contracts.TokenLifetime, ResendInterval: contracts.ResetInterval})
	pkit.Put[usercontracts.InvitationAuthority](w, &usercontracts.InvitationAuthorityFunc{})
	return manifest, nil
}

type roleGranting struct{ authorize contracts.GrantAuthorizer }

func (g roleGranting) May(ctx context.Context, tx db.Tx[db.Tenant]) (bool, error) {
	return g.authorize.Allowed(ctx, tx, tenancy.Grant{Permission: contracts.PermissionRoleManage})
}

func withGrants(deps Deps) Deps {
	if (deps.Authorization == nil) != (deps.Permissions == nil) {
		panic("auth: supply both Authorization and Permissions, or leave both nil")
	}
	if deps.Authorization == nil {
		deps.Authorization, deps.Permissions, _ = NewGrants(GrantsDeps{})
	}
	return deps
}
