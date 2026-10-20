package main

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/module"
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
	pkit.Needs[usercontracts.Service](),
	pkit.Needs[authcontracts.Auth](),
	pkit.Provides[scimcontracts.Service](),
)

func wireScim(w *pkit.Wiring) (module.Module, error) {
	auth := pkit.Get[authcontracts.Auth](w)
	svc, manifest := scim.New(scim.Deps{
		Users: pkit.Get[usercontracts.Service](w),
		Roles: roleNames{auth: auth},
		// The same two tables, asked a different question: this one is about the
		// caller rather than about a name.
		Authority: holderGrants{
			users: pkit.Get[usercontracts.Service](w),
			auth:  auth,
		},
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

// holderGrants is the adapter between the scim module's question about its caller
// and the two tables that answer it: the person's roles are modules/user's row,
// and what those roles grant is auth's.
//
// It reads, and never caches, for the reason roleNames does above: a mapping is a
// grant decided in advance, and the day a role stops being held or starts granting
// something has to be the day the answer changes — not the day somebody restarts a
// process. It resolves the person's roles from the row rather than from whatever
// the principal claims, because that row is what the session this principal came
// from was built from (auth.Identify reads it on every request) and reading it here
// means a caller cannot be its own authority by naming its own roles.
//
// A credential that carries its own ceiling is answered from that ceiling, by the
// kernel's own rule and in the same words auth's authorizer uses: a scoped key was
// minted narrower than its holder on purpose, and resolving the holder's roles here
// would answer a question about the person instead of about the caller. The
// intersection the key carries is already that recomputation, done when the request
// resolved the key.
//
// Nobody is refused, not failed: a write that names no caller, or names a person
// this tenant has no active row for, has no authority to ask about, and the write
// that needs it is refused for the ordinary reason. An error is left for what is
// actually an outage — a database that cannot be read — because the module reads an
// error as "this write cannot be decided" and refuses, and laundering a denial into
// one would make an outage read as a policy.
type holderGrants struct {
	users usercontracts.Service
	auth  authcontracts.Auth
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
