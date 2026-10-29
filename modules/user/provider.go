package user

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/internal"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module declares account capabilities and adapters over one service. Granting
// and Administration must be provided before construction; no session service is needed.
var Module = pkit.NewModule("user", wire,
	pkit.Needs[usercontracts.Administration](),
	pkit.Needs[usercontracts.Granting](),
	pkit.Optional[usercontracts.Clock](),
	pkit.Optional[usercontracts.InvitationTiming](),
	pkit.Optional[usercontracts.InvitationAuthority](),
	pkit.Provides[usercontracts.Service](),
	pkit.Provides[usercontracts.InvitationCommands](),
	pkit.Provides[usercontracts.InvitationReader](),
	pkit.Provides[usercontracts.InvitationRetention](),
	pkit.Provides[authcontracts.InvitationUsers](),
	pkit.Provides[usercontracts.Lookup](),
	pkit.Provides[usercontracts.SystemProvisioner](),
	pkit.Provides[usercontracts.RegistrationUsers](),
	pkit.Provides[usercontracts.EmailRegistrar](),
	pkit.Provides[usercontracts.PendingRegistrar](),
	pkit.Provides[usercontracts.RetainedLookup](),
	pkit.Provides[usercontracts.Provisioning](),
	pkit.Provides[usercontracts.RoleHolders](),
	pkit.Provides[usercontracts.AddressLookup](),
	pkit.Provides[usercontracts.SeedWriter](),

	pkit.Provides[authcontracts.Users](),
	pkit.Provides[authcontracts.Provisioner](),
	pkit.Provides[notificationcontracts.RecipientLookup](),
	pkit.Provides[tenantcontracts.Inviter](),
	pkit.Contributes[personaldata.Part](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	svc, manifest := New(Deps{
		Administration:      pkit.Get[usercontracts.Administration](w),
		Granting:            pkit.Get[usercontracts.Granting](w),
		Clock:               pkit.Get[usercontracts.Clock](w),
		InvitationTiming:    pkit.Get[usercontracts.InvitationTiming](w),
		InvitationAuthority: pkit.Get[usercontracts.InvitationAuthority](w),
	})
	pkit.Put(w, svc)
	pkit.Put[usercontracts.InvitationCommands](w, svc.(*internal.Service))
	pkit.Put[usercontracts.InvitationReader](w, svc.(*internal.Service))
	pkit.Put[usercontracts.InvitationRetention](w, svc.(*internal.Service))
	pkit.Put[authcontracts.InvitationUsers](w, svc.(*internal.Service))
	pkit.Put[usercontracts.Lookup](w, svc)
	pkit.Put[usercontracts.SystemProvisioner](w, svc)
	pkit.Put[usercontracts.RegistrationUsers](w, svc)
	pkit.Put[usercontracts.EmailRegistrar](w, svc)
	pkit.Put[usercontracts.PendingRegistrar](w, svc)
	pkit.Put[usercontracts.RetainedLookup](w, svc)
	pkit.Put[usercontracts.Provisioning](w, svc)
	pkit.Put[usercontracts.RoleHolders](w, svc)
	pkit.Put[usercontracts.AddressLookup](w, svc)
	pkit.Put[usercontracts.SeedWriter](w, svc)

	pkit.Put[authcontracts.Users](w, svc)
	pkit.Put[authcontracts.Provisioner](w, provisioner{users: svc})
	pkit.Put[notificationcontracts.RecipientLookup](w, recipients{users: svc})
	pkit.Put[tenantcontracts.Inviter](w, firstAdmin{users: svc})
	// The one place in this composition that reads a concrete type back out of an
	// interface: New builds the *internal.Service and hands it out as the contract, and
	// the erasure needs the service's own floor, not a second copy of the rule.
	pkit.Put[personaldata.Part](w, internal.PersonalData(svc.(*internal.Service)))
	return manifest, nil
}

// recipients answers the notification module's RecipientLookup over a person's
// row, in the worker's own tenant transaction. It lives here rather than in the
// composition because the read is this module's and the port is the consumer's:
// notification still names no module, and a client that composes both gets the
// answer without writing it.
type recipients struct{ users usercontracts.Lookup }

func (r recipients) Email(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (string, error) {
	u, err := r.users.Get(ctx, tx, userID)
	if err != nil {
		return "", err
	}
	return u.Email, nil
}

// firstAdmin is the adapter behind POST /api/v1/tenant/tenants/{id}/invite.
//
// Provision with no password is the whole of it, and the two halves of that are
// deliberate. No password, so an operator who invites somebody into a customer's
// tenant does not know their credentials and never held them; the invitation
// event mails a link and the person chooses one. The admin role, because the
// route's purpose is a tenant that somebody can administer — an invitation that
// granted nothing would be a tenant still nobody can get into, which is the
// defect this route exists to close.
//
// It runs in the control plane's own system transaction, which is the only way
// to write a row into a tenant the request did not resolve to.
type firstAdmin struct {
	users usercontracts.SystemProvisioner
}

func (a firstAdmin) Invite(ctx context.Context, tx db.Tx[db.System], tenantID uuid.UUID, email, displayName string) error {
	_, err := a.users.Provision(ctx, tx, tenantID, email, displayName, "",
		[]string{authcontracts.RoleAdmin})
	return err
}

// provisioner answers the auth module's Provisioner port, for the tenant whose
// registration mode is `provision`. Both calls run in the request's tenant
// transaction, so the person and the roles are made or none is: an unknown role
// name is refused by this module, the transaction aborts, and no half-made
// person is left behind. The address the identity provider verified is what the
// row is made from, and the confirmation the callback records is what makes it
// able to sign in — this hands out no password here, because a secret nobody
// chose is not a credential.
//
// Which roles the mode hands out is the tenant operator's decision, taken at
// POST /tenants/{id}/oidc and stored in the tenant's row; this line decides only
// that the roles named there and no others are the ones the person arrives with.
type provisioner struct {
	users usercontracts.InvitationWriter
}

func (p provisioner) Provision(ctx context.Context, tx db.Tx[db.Tenant], email, displayName string, roles []string) (uuid.UUID, error) {
	person, err := p.users.Invite(ctx, tx, email, displayName)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := p.users.SetRoles(ctx, tx, person.ID, roles); err != nil {
		return uuid.Nil, err
	}
	return person.ID, nil
}
