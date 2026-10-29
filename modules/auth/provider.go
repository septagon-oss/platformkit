package auth

import (
	"fmt"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module declares session capabilities and their independent grant dependencies.
var Module = pkit.NewModule("auth", wire,
	pkit.Needs[authcontracts.Users](),
	pkit.Needs[authcontracts.Provisioner](),
	pkit.Needs[authcontracts.Notifier](),
	pkit.Needs[authcontracts.Mailer](),
	// Whether that sender delivers. Needed rather than Optional with a default of
	// "yes": a composition that composes auth and forgets notification is a defect
	// in the composition, and the plan refuses it at boot rather than booting a
	// server that refuses every sign-up forever.
	pkit.Needs[authcontracts.MailDelivery](),
	pkit.Needs[authcontracts.Hosts](),
	pkit.Needs[authcontracts.MailLedger](),
	pkit.Needs[authcontracts.OIDCProviders](),
	pkit.Needs[jobs.TenantLister](),
	pkit.Needs[authcontracts.GrantAuthorizer](),
	pkit.Needs[authcontracts.PermissionReader](),
	pkit.Optional[usercontracts.Clock](),
	pkit.Optional[usercontracts.InvitationReader](),
	pkit.Optional[usercontracts.InvitationAuthority](),
	pkit.Optional[authcontracts.InvitationUsers](),
	// Optional, not Needed: with no tenant module there is no tenant whose IdP could
	// be resolved and no installation-level default to fall back to, so auth mounts
	// no SAML leg at all rather than three doors that answer 404 for everybody.
	pkit.Optional[authcontracts.SAMLProviders](),
	pkit.Optional[authcontracts.RegistrationMode](),
	pkit.Optional[authcontracts.ConfirmationChrome](),
	pkit.Optional[LinkPages](),
	pkit.Provides[authcontracts.Auth](),
	pkit.Provides[authcontracts.RoleEditor](),
	pkit.Provides[authcontracts.SessionAdministration](),
	pkit.Provides[authcontracts.RoleReader](),
	pkit.Provides[authcontracts.RoleRepairer](),
	pkit.Provides[authcontracts.InvitationLinks](),
	pkit.Provides[authcontracts.InvitationReadings](),

	pkit.Provides[httpx.Authorizer](),
	pkit.Provides[pkit.Authenticator](),
	pkit.Contributes[personaldata.Part](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	settings := pkit.Config(w, func(c config.Config) config.Auth { return c.Auth })
	server := pkit.Config(w, func(c config.Config) config.Server { return c.Server })
	deps := Deps{
		Authorization:    pkit.Get[authcontracts.GrantAuthorizer](w),
		Permissions:      pkit.Get[authcontracts.PermissionReader](w),
		Clock:            pkit.Get[usercontracts.Clock](w),
		InvitationUsers:  pkit.Get[authcontracts.InvitationUsers](w),
		InvitationReader: pkit.Get[usercontracts.InvitationReader](w),
		Users:            pkit.Get[authcontracts.Users](w),
		Notify:           pkit.Get[authcontracts.Notifier](w),
		// The same sender and the same host lookup the notification module
		// takes, handed to the one module that has to put a secret in a message
		// without it becoming a row first: a set-password link belongs in the
		// mail and in nothing else.
		Mailer: pkit.Get[authcontracts.Mailer](w),
		// The same carrier's own answer, so that "a mailer is wired" cannot be read
		// as "a mail will go out".
		MailDelivery: pkit.Get[authcontracts.MailDelivery](w),
		Hosts:        pkit.Get[authcontracts.Hosts](w),
		// The same service Notify is resolved from, under the ledger's own key:
		// the record of the two mails this module sends itself, so that a send
		// leaves a row before the mail can be read back at the delivery door.
		Mails:   pkit.Get[authcontracts.MailLedger](w),
		Tenants: pkit.Get[jobs.TenantLister](w),
		// The installation's own provider is the fallback; the tenant's row wins
		// where it names one, which is what lets two tenants on this one process
		// send their people to two issuers. The secret is resolved from the
		// environment by reference, per request, so it is in no row, no outbox
		// payload and no audit record.
		OIDC:          OIDCFromConfig(settings.OIDC),
		OIDCProviders: pkit.Get[authcontracts.OIDCProviders](w),
		// Optional rather than Needed, and the reason is the shape of the answer: a
		// composition with no tenant module has no tenant whose IdP could be resolved,
		// and SAML has no installation-level default to fall back to. It then mounts no
		// SAML leg at all, which is what the surface says out loud.
		SAMLProviders: pkit.Get[authcontracts.SAMLProviders](w),
		Secrets:       EnvironmentSecrets{},
		// A tenant that sets `provision` has said, at its own control-plane
		// route, that an address its provider verified is an account here. The
		// person is made over the user module, with the roles the tenant's row
		// names and no others. A deployment that would rather not admit anyone
		// composes no Provisioner, and auth then answers every tenant as
		// `existing`: a refusal, and not a half-made person.
		Provisioner: pkit.Get[authcontracts.Provisioner](w),
		// The words the passkey doors answer a refusal in. This is the whole
		// catalogue the application named with Languages, this module's own words
		// included: a browser ceremony is read on the sign-in page, whose copy is
		// translated, and a module that read only its own messages file could not
		// answer in a language a UI catalogue carries.
		Messages: w.Skin().Copy,
		// Empty FactorKey leaves the second-factor routes unmounted, as it does
		// today: without a key the only secret this module could write is a
		// plaintext one.
		FactorKey: settings.FactorKey,
		// The account-request budget the file names; none named is the module's
		// own ten per fifteen minutes.
		AccountRequests: accountRequestsFromConfig(settings.AccountRequests),
		// The pages the two mailed links open, when the composition names its own
		// with MailedLinksOpen; none composed is the admin shell's screens.
		LinkPages:  pkit.Get[LinkPages](w),
		PublicHost: server.PublicHost,
	}
	mailedLinksOpen(&deps, w)
	// A file-borne budget the counter cannot keep is the plan's refusal, not
	// New's panic: the loader checked presence, and only the counter knows its
	// horizon.
	if err := deps.AccountRequests.Check(); err != nil {
		return module.Module{}, fmt.Errorf("auth: auth.account_requests: %w", err)
	}
	svc, manifest := New(deps)
	pkit.Put(w, svc)
	if authority := pkit.Get[usercontracts.InvitationAuthority](w); authority != nil {
		if holder, ok := authority.(*usercontracts.InvitationAuthorityFunc); ok && holder != nil {
			holder.Ask = svc.(*internal.Service).InspectInvitationAuthority
		}
	}
	pkit.Put[authcontracts.InvitationLinks](w, svc.(*internal.Service))
	pkit.Put[authcontracts.InvitationReadings](w, svc.(*internal.Service))
	pkit.Put[authcontracts.RoleEditor](w, svc)
	pkit.Put[authcontracts.SessionAdministration](w, svc)
	pkit.Put[authcontracts.RoleReader](w, svc)
	pkit.Put[authcontracts.RoleRepairer](w, svc)

	pkit.Put[httpx.Authorizer](w, svc)
	pkit.Put[pkit.Authenticator](w, svc.Authenticate)
	pkit.Put[personaldata.Part](w, internal.PersonalData())
	return manifest, nil
}

// mailedLinksOpen writes the two Deps fields whose value is not a service the
// composition resolved but a choice this installation made: the page a mailed link
// opens, whose two addresses come from the composition — the only party that knows
// what it mounted — and whose colours and words come from the skin the resolved
// composition answered with, the same value the admin shell and the public site
// draw themselves from; and which public signup door this installation mounted,
// which is a composed module of its own (Registration, EmailRegistration,
// ApprovalRegistration) because three mutually exclusive Deps fields were one
// decision written three ways. Both halves are optional, and an installation that
// named neither gets what an empty Deps field always gave: a bare document, and no
// door.
func mailedLinksOpen(deps *Deps, w *pkit.Wiring) {
	chrome := pkit.Get[authcontracts.ConfirmationChrome](w)
	if chrome.Assets != "" || chrome.SignIn != "" {
		skin := w.Skin()
		deps.Pages = Pages{
			Theme: skin.Theme, Assets: chrome.Assets, SignIn: chrome.SignIn, Messages: skin.Copy,
		}
	}
	switch mode := pkit.Get[authcontracts.RegistrationMode](w).(type) {
	case authcontracts.EmailRegistrationMode:
		deps.EmailRegistration = mode.Policy
	case authcontracts.ApprovalRegistrationMode:
		deps.ApprovalRegistration = mode.Policy
	case authcontracts.PasswordRegistration:
		deps.Registration = mode.Users
	}
}

// MailedLinksOpen is how a composition names the pages its mailed links open:
// `Use(auth.Module, auth.MailedLinksOpen(auth.LinkPages{SetPassword: "/account/password"}))`.
// A page New would refuse fails the build here, before auth is built and before
// anything opens.
func MailedLinksOpen(pages LinkPages) *pkit.Module {
	return pkit.NewModule("authlinks", func(w *pkit.Wiring) (module.Module, error) {
		if err := pages.check(); err != nil {
			return module.Module{}, err
		}
		pkit.Put(w, pages)
		return module.Module{Name: "authlinks"}, nil
	}, pkit.Provides[LinkPages]())
}

// The three public signup doors. Each is a module of its own, so the app's
// sentence names the one it opens (`Use(auth.Module, auth.EmailRegistration)`)
// and composing two is the ambiguity Choose answers rather than a panic about
// two fields that cannot coexist. The roles a registered account arrives with
// are named here and never read from the form, and the least of the two the seed
// provisions is the one each names.

// Registration is member signup with emailed password setup.
var Registration = door("registration", func(users usercontracts.RegistrationUsers) authcontracts.RegistrationMode {
	return authcontracts.PasswordRegistration{Users: users, Roles: []string{authcontracts.RoleMember}}
})

// EmailRegistration is the password door that also requires mailbox
// confirmation. A composition that turns it off takes the three inquiry aliases
// out with it — /api/v1/auth/register, /resend-verification and /verify-email —
// and the case that asks the running server each one answers says so.
var EmailRegistration = door("emailregistration", func(users usercontracts.EmailRegistrar) authcontracts.RegistrationMode {
	policy, err := (&authcontracts.EmailRegistration{Users: users, Roles: []string{authcontracts.RoleMember}}).Checked()
	if err != nil {
		panic(err)
	}
	return authcontracts.EmailRegistrationMode{Policy: &policy}
})

// ApprovalRegistration accepts a password, a confirmation and a terms consent
// and keeps the account pending for review. It needs no mail delivery.
var ApprovalRegistration = door("approvalregistration", func(users usercontracts.PendingRegistrar) authcontracts.RegistrationMode {
	policy, err := (&authcontracts.ApprovalRegistration{Users: users, Roles: []string{authcontracts.RoleMember}}).Checked()
	if err != nil {
		panic(err)
	}
	return authcontracts.ApprovalRegistrationMode{Policy: &policy}
})

// door is the shape all three share: the people it writes through come from the
// user module, and what it puts is the finished policy, so no half-filled door
// is ever mounted.
func door[T any](name string, mode func(T) authcontracts.RegistrationMode) *pkit.Module {
	return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[authcontracts.RegistrationMode](w, mode(pkit.Get[T](w)))
		return module.Module{Name: name}, nil
	},
		pkit.Needs[T](),
		pkit.Provides[authcontracts.RegistrationMode](),
	)
}
