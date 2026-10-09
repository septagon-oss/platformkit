package auth

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the front door as the resolver sees it, for an application that
// names it in Use: `Use(auth.Module)`.
//
// Three contracts come out of it, and they are the three a composition used to
// answer for itself: Auth is the service, httpx.Authorizer is the answer every
// route is enforced with, and pkit.Authenticator is who is calling. Before this
// apps/platformkit put all three in a module of its own making; they are put
// here, by the module that owns the service, by every application that composes
// it — which is what makes "one provider each, named in the composition file"
// true rather than a comment saying so.
//
// The one thing it reads that it does not consume is usercontracts.Granting.
// Reading it is what places auth after the holder exists; filling its Ask is the
// answer. That edge is late-bound rather than built — the user module is built
// before auth, because auth looks people up — and a declaration in either
// direction would be the cycle user → auth → user, which is a fact about the
// graph and no composition edits it. pkit/late_bound_holder_test.go is the
// mechanism's own case; refusing when nothing filled the holder is pinned here
// rather than left to the first roles write.
var Module = pkit.NewModule("auth", wire,
	pkit.Needs[authcontracts.Users](),
	pkit.Needs[authcontracts.Provisioner](),
	pkit.Needs[authcontracts.Notifier](),
	pkit.Needs[authcontracts.Mailer](),
	pkit.Needs[authcontracts.Hosts](),
	pkit.Needs[authcontracts.OIDCProviders](),
	pkit.Needs[jobs.TenantLister](),
	pkit.Needs[usercontracts.Granting](),
	// Optional, not Needed: with no tenant module there is no tenant whose IdP could
	// be resolved and no installation-level default to fall back to, so auth mounts
	// no SAML leg at all rather than three doors that answer 404 for everybody.
	pkit.Optional[authcontracts.SAMLProviders](),
	pkit.Optional[authcontracts.RegistrationMode](),
	pkit.Optional[authcontracts.ConfirmationChrome](),
	pkit.Provides[authcontracts.Auth](),
	pkit.Provides[httpx.Authorizer](),
	pkit.Provides[pkit.Authenticator](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	settings := pkit.Config(w, func(c config.Config) config.Auth { return c.Auth })
	server := pkit.Config(w, func(c config.Config) config.Server { return c.Server })
	granting := pkit.Get[usercontracts.Granting](w)
	if granting == nil {
		return module.Module{}, fmt.Errorf("auth: composes no usercontracts.Granting, which is the door a roles write hands out an administering role through: compose the user module's, or one of your own")
	}
	deps := Deps{
		Users:  pkit.Get[authcontracts.Users](w),
		Notify: pkit.Get[authcontracts.Notifier](w),
		// The same sender and the same host lookup the notification module
		// takes, handed to the one module that has to put a secret in a message
		// without it becoming a row first: a set-password link belongs in the
		// mail and in nothing else.
		Mailer:  pkit.Get[authcontracts.Mailer](w),
		Hosts:   pkit.Get[authcontracts.Hosts](w),
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
		FactorKey:  settings.FactorKey,
		PublicHost: server.PublicHost,
	}
	// The page the verification link opens. Its two addresses come from the
	// composition, which is the only party that knows what it mounted; its
	// colours and its words come from the skin the resolved composition answered
	// with, which is the same value the admin shell and the public site draw
	// themselves from. An application that names neither gets a bare document,
	// as an empty Deps field always gave.
	chrome := pkit.Get[authcontracts.ConfirmationChrome](w)
	if chrome.Assets != "" || chrome.SignIn != "" {
		skin := w.Skin()
		deps.Pages = Pages{
			Theme: skin.Theme, Assets: chrome.Assets, SignIn: chrome.SignIn, Messages: skin.Copy,
		}
	}
	// Which public signup door this installation mounted is a composed module of
	// its own (Registration, EmailRegistration, ApprovalRegistration), because
	// three mutually exclusive Deps fields were one decision written three ways.
	// None composed means no door, which is what the empty field always meant.
	switch mode := pkit.Get[authcontracts.RegistrationMode](w).(type) {
	case authcontracts.EmailRegistrationMode:
		deps.EmailRegistration = mode.Policy
	case authcontracts.ApprovalRegistrationMode:
		deps.ApprovalRegistration = mode.Policy
	case authcontracts.PasswordRegistration:
		deps.Registration = mode.Users
	}
	svc, manifest := New(deps)
	// The fill, and the whole of the late-bound edge. May the caller of a roles
	// write hand out a role that administers the tenant is the authorizer's
	// answer, asked of this module's own permission, in the request's own tenant
	// transaction — the same question every route is asked, one line earlier and
	// about a different act.
	fillGranting(granting, svc)
	pkit.Put(w, svc)
	pkit.Put[httpx.Authorizer](w, svc)
	pkit.Put[pkit.Authenticator](w, svc.Authenticate)
	return manifest, nil
}

// fillGranting writes the one field of the composition's Granting holder that
// only this module can answer. It is a fill and not a Put: the holder is read by
// the user module, which is built before this one.
//
// A holder with no Ask refuses every promotion (contracts.GrantingFunc.May
// answers the error), and this runs in the same build that mounts auth's routes,
// so the refusal is a composition defect fixed at boot and not a door that opens
// for whoever happens to be sitting in front of it.
func fillGranting(g usercontracts.Granting, svc authcontracts.Auth) {
	held, ok := g.(*usercontracts.GrantingFunc)
	if !ok || held == nil {
		// A hand-written Granting answers its own question; there is nothing
		// here to fill, and the composition that wrote it decided who may
		// promote in its own file.
		return
	}
	held.Ask = func(ctx context.Context, tx db.Tx[db.Tenant]) (bool, error) {
		p, hasPrincipal := tenancy.PrincipalFrom(ctx)
		t, hasTenant := tenancy.FromContext(ctx)
		if !hasPrincipal || p.UserID == uuid.Nil || !hasTenant {
			return false, nil
		}
		return svc.Allowed(ctx, t, tenancy.Grant{Permission: authcontracts.PermissionRoleManage})
	}
}

// The three public signup doors. Each is a module of its own, so the app's
// sentence names the one it opens (`Use(auth.Module, auth.EmailRegistration)`)
// and composing two is the ambiguity Choose answers rather than a panic about
// two fields that cannot coexist. The roles a registered account arrives with
// are named here and never read from the form, and the least of the two the seed
// provisions is the one each names.

// Registration is member signup with emailed password setup.
var Registration = door("registration", func(users usercontracts.Service) authcontracts.RegistrationMode {
	return authcontracts.PasswordRegistration{Users: users, Roles: []string{authcontracts.RoleMember}}
})

// EmailRegistration is the password door that also requires mailbox
// confirmation. A composition that turns it off takes the three inquiry aliases
// out with it — /api/v1/auth/register, /resend-verification and /verify-email —
// and the case that asks the running server each one answers says so.
var EmailRegistration = door("emailregistration", func(users usercontracts.Service) authcontracts.RegistrationMode {
	policy, err := (&authcontracts.EmailRegistration{Users: users, Roles: []string{authcontracts.RoleMember}}).Checked()
	if err != nil {
		panic(err)
	}
	return authcontracts.EmailRegistrationMode{Policy: &policy}
})

// ApprovalRegistration accepts a password, a confirmation and a terms consent
// and keeps the account pending for review. It needs no mail delivery.
var ApprovalRegistration = door("approvalregistration", func(users usercontracts.Service) authcontracts.RegistrationMode {
	policy, err := (&authcontracts.ApprovalRegistration{Users: users, Roles: []string{authcontracts.RoleMember}}).Checked()
	if err != nil {
		panic(err)
	}
	return authcontracts.ApprovalRegistrationMode{Policy: &policy}
})

// door is the shape all three share: the people it writes through come from the
// user module, and what it puts is the finished policy, so no half-filled door
// is ever mounted.
func door(name string, mode func(usercontracts.Service) authcontracts.RegistrationMode) *pkit.Module {
	return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[authcontracts.RegistrationMode](w, mode(pkit.Get[usercontracts.Service](w)))
		return module.Module{Name: name}, nil
	},
		pkit.Needs[usercontracts.Service](),
		pkit.Provides[authcontracts.RegistrationMode](),
	)
}
