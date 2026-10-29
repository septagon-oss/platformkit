package main

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	scim "github.com/septagon-oss/platformkit/modules/scim"
	scimcontracts "github.com/septagon-oss/platformkit/modules/scim/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// The identity-provider door, wired.
//
// The scim module names three things it cannot make for itself: the person, which
// is modules/user's service, and two questions about this tenant's authority —
// "does this tenant know that role" and "does this caller hold that permission",
// which are the auth module's table and, for a session, that table read over the
// roles modules/user owns. A provisioning module that read either would be one
// query across two modules' rows, so the composition answers both, over the two
// methods it already holds: auth's list of this tenant's role names and auth's
// union of what a set of roles grants.
var scimProvisioning = pkit.NewModule("scim", wireScim,
	pkit.Needs[usercontracts.Provisioning](),
	pkit.Needs[usercontracts.Lookup](),
	pkit.Needs[authcontracts.RoleReader](),
	pkit.Needs[authcontracts.PermissionReader](),
	pkit.Provides[scimcontracts.Service](),
	// The person's copy and the person's erasure reach this door's rows too: the
	// identifier a provisioning client sent may be the person's own address, and
	// modules/subject is the one caller that can ask every owner at once.
	pkit.Contributes[personaldata.Part](),
)

func wireScim(w *pkit.Wiring) (module.Module, error) {
	svc, manifest, err := scim.NewChecked(scim.Deps{
		Provisioning: pkit.Get[usercontracts.Provisioning](w),
		Roles:        roleNames{auth: pkit.Get[authcontracts.RoleReader](w)},
		// The same two tables, asked a different question: this one is about the
		// caller rather than about a name.
		Authority: holderGrants{
			users: pkit.Get[usercontracts.Lookup](w),
			auth:  pkit.Get[authcontracts.PermissionReader](w),
		},
	})

	if err != nil {
		return module.Module{}, err
	}
	pkit.Put[scimcontracts.Service](w, svc)
	pkit.Put[personaldata.Part](w, scim.PersonalData())
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
type roleNames struct{ auth authcontracts.RoleReader }

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

// holderGrants is the adapter between the scim module's question about its caller and
// the two tables that answer it: the person's roles are modules/user's row, and what
// those roles grant is auth's.
//
// It reads, and never caches, for the reason roleNames does above: a mapping is a
// grant decided in advance, and the day a role stops being held has to be the day the
// answer changes, not the day somebody restarts a process. Roles come from the row,
// so a caller cannot be its own authority by naming its own roles.
//
// A credential that carries its own ceiling is answered from that ceiling, in the
// words auth's authorizer uses: a scoped key was minted narrower on purpose, and the
// intersection it carries is already the recomputation. Nobody is refused, not failed:
// an error is left for an outage, since a denial laundered into one reads as policy.
type holderGrants struct {
	users usercontracts.Lookup
	auth  authcontracts.PermissionReader
}

func (g holderGrants) Holds(ctx context.Context, tx db.Tx[db.Tenant], permission string) (bool, error) {
	p, ok := tenancy.PrincipalFrom(ctx)
	if !ok || p.UserID == uuid.Nil {
		return false, nil
	}
	grant := tenancy.Grant{Permission: permission}
	if p.Permissions != nil {
		return authcontracts.Grants(authcontracts.Permissions(p.Permissions), grant), nil
	}
	u, err := g.users.Get(ctx, tx, p.UserID)
	if errors.Is(err, crud.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// A person who cannot sign in holds nothing here, which is auth's rule about a
	// session and is stated here because a direct caller of MapGroup did not pass
	// through the door that applies it.
	if u.Status != usercontracts.StatusActive {
		return false, nil
	}
	held, err := g.auth.Permissions(ctx, tx, u.Roles)
	if err != nil {
		return false, err
	}
	return authcontracts.Grants(held, grant), nil
}
