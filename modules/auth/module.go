// Package auth is signing in: sessions, passwords, single sign-on, and the
// roles that decide what a caller may do.
//
// The application constructs tenants with SeedRoles before wiring notification
// delivery and authentication. Role provisioning needs no running auth service.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// OIDCFromConfig converts the application's own configuration block into this
// module's struct, so that main writes one line and the module depends on a
// struct of its own rather than on the configuration surface. The registration
// mode is not in it: the mode is a tenant's fact now, and the installation's own
// default provider has never had one to declare.
func OIDCFromConfig(c config.OIDC) OIDC {
	return OIDC{Issuer: c.Issuer, ClientID: c.ClientID, ClientSecret: c.ClientSecret,
		RedirectPath: c.RedirectPath}
}

// accountRequestsFromConfig converts the application's own account-request block
// into this module's budget, the way OIDCFromConfig does: an omitted key is the
// zero field, which is the module's default, and a present value is the value.
// Presence is the loader's to judge — config.Load has refused a present zero —
// so a Config built in Go that skips Load reads an explicit zero as the default.
func accountRequestsFromConfig(c config.AccountRequests) contracts.AccountRequestBudget {
	var b contracts.AccountRequestBudget
	if c.Quota != nil {
		b.Quota = *c.Quota
	}
	if c.Window != nil {
		b.Window = *c.Window
	}
	return b
}

// EnvironmentSecrets is the Secrets this installation ships with: a reference is
// an environment variable's name, which is the only secret store this repository
// has (kit/config reads secrets from the environment and nowhere else). It is a
// method on an empty struct rather than a function value because a composition
// should be able to name the whole of what it wires.
type EnvironmentSecrets struct{}

// Lookup reads the variable the reference names. An unset name is false, and the
// sign-in answers 503 for that tenant rather than exchanging a code with an empty
// client secret.
func (EnvironmentSecrets) Lookup(_ context.Context, ref string) (string, bool) {
	return os.LookupEnv(ref)
}

// OIDC is one OpenID Connect provider. It has the same shape as config.OIDC, so
// main converts one to the other in a line, and this module depends on a struct
// of its own rather than on the application's configuration surface.
type OIDC = internal.OIDC

// Pages is the chrome of the one page this module writes for itself: the page
// the address in a verification email opens (see internal/ui/page.go).
//
// The page is the module's — it is the credential, the state and the command
// that spends them — and its chrome is not: the palette, where the stylesheet is
// served and where a person who has just confirmed an address is sent are facts
// about the installation, so the composition writes them here. A composition
// that turns emailed registration on and leaves Pages zero mails a link that
// opens nothing.
type Pages = internal.Pages

// Deps is what this module cannot make for itself.
type Deps struct {
	// Authorization and Permissions must be supplied together. Leaving both nil
	// constructs the independent default policy for legacy direct callers.
	Authorization contracts.GrantAuthorizer
	Permissions   contracts.PermissionReader

	// Users is how a password login finds the person an address belongs to,
	// and how a password that has been earned is stored. It is narrower than
	// the user module's own Service: a consumer depends on the capability it
	// uses.
	Users contracts.Users

	// Registration opts this composition into public member signup with emailed
	// password setup. Mailer and Hosts must be configured before requests can
	// be accepted. Without a registration capability, signup is disabled.
	Registration contracts.RegistrationUsers

	// ApprovalRegistration accepts a password, confirmation and terms consent
	// and keeps the account pending for review. Initial roles are application
	// defaults. It needs no mail delivery and cannot coexist with Registration.
	ApprovalRegistration *contracts.ApprovalRegistration

	// EmailRegistration accepts a password and requires independent mailbox
	// confirmation. It needs email delivery and excludes both modes above.
	EmailRegistration *contracts.EmailRegistration

	// Notify is how somebody is told, inside the application, that a link was
	// sent. It never carries the link: the notice points at the set-password
	// page (LinkPages), the secret is in the mail and nowhere else. A
	// composition that wires none still sends the mail.
	Notify contracts.Notifier

	// Mailer is where the link itself goes, and it is the only place it goes.
	// A composition that wires none issues no token either — a link nobody is
	// sent is a live credential in a table for an hour, for nothing — and the
	// forgotten-password route still answers as though it had.
	//
	// It is the notification module's own Mailer, wired by the application to
	// the same sender everything else uses, because this module needs to hand a
	// message over without it becoming a row first. See contracts.Mailer.
	Mailer contracts.Mailer

	// Hosts turns the tenant a link belongs to into the host its people reach
	// the application at, which a mailed link has to be built on: a link on the
	// installation's public host would send one customer's people to another
	// customer's front door. It is the notification module's own lookup, wired
	// by the application over the tenant module.
	Hosts contracts.Hosts

	// Mails is what this module records about the mails it sends itself: one row
	// per send, with no subject, body, link or credential in it. It is the
	// notification module's own ledger of them, named here the way Mailer and Hosts
	// are, so Deps reads as one list of capabilities. A composition that wires none
	// sends exactly as it did before the record existed and leaves no trace of the
	// attempt — which is the bug this closes, not a mode to preserve.
	Mails contracts.MailLedger

	// MailDelivery is whether the sender above attempts what it is handed. It is
	// required, and it is fail-closed: nil reads as "nothing is delivered", the
	// same answer a deployment with no mail host gives, because a port nobody
	// wired is not evidence that mail goes out.
	//
	// The doors that would promise a person an email — sign-up, resend
	// verification, forgotten password — refuse when it says no, and a composition
	// that composes auth is refused at the plan unless something answers it.
	MailDelivery contracts.MailDelivery

	// Tenants is how the hourly sweep reaches every tenant, to delete the
	// sessions and tokens that have expired.
	Tenants jobs.TenantLister

	// OIDC is the installation's own identity provider, and the fallback for a
	// tenant that names none. An empty issuer means there is no default, and
	// then the two OIDC routes are mounted only when OIDCProviders is wired —
	// which is the case that lets two tenants sign in at two issuers in one
	// process, each answering for its own host.
	OIDC OIDC

	// SAMLProviders answers "which SAML 2.0 identity provider does the tenant this
	// request resolved to sign in against?", per request, from the transaction the
	// Host header chose. Unlike OIDC there is no installation-level default to fall
	// back to — no `kit/config.SAML` block exists, because SAML ships tenant-only
	// and a single-IdP deployment configures its one tenant's row — so this port
	// being nil is the whole of "nobody here signs in with SAML", and the three SAML
	// legs are then not mounted at all.
	SAMLProviders contracts.SAMLProviders

	// OIDCProviders answers "which provider does the tenant this request
	// resolved to sign in against?", per request, from the transaction the Host
	// header chose. A composition wires it over the tenant module's OIDCOf; a
	// composition that wires nothing leaves the installation's own issuer as the
	// only one there is, which is exactly how every deployment that exists today
	// behaves.
	OIDCProviders contracts.OIDCProviders

	// FactorKey is the deployment's key for sealing a second factor's shared
	// secret at rest. Empty — which is the default — means the factor routes are
	// not mounted at all: without a key the only secret this module could write
	// is a plaintext one, and a table of those is a backup an attacker can use,
	// which is the thing this capability exists not to do. A deployment that sets
	// no key signs people in exactly as it did before the table existed.
	FactorKey string

	// AccountRequests is how many account requests — a sign-up, a verification
	// resend, a forgotten-password link — one address may make in a window. The
	// zero value is the module's own ten per fifteen minutes, which is what every
	// composition had before the field existed; New refuses a budget the counter
	// cannot keep. See contracts.AccountRequestBudget.
	AccountRequests contracts.AccountRequestBudget

	// Secrets resolves the reference a tenant's row holds into the client secret
	// it names. The reference is what is in the database, the outbox and the
	// audit trail; the secret is in the environment and in no row. A composition
	// that wires none can sign nobody in through a tenant's own provider, and
	// answers 503 rather than inventing an empty secret.
	Secrets contracts.Secrets

	// Provisioner makes the person a verified id token names when the tenant's
	// registration mode is `provision`. Wiring it is the composition's answer to
	// "may this installation create people from an IdP claim", because who may
	// exist is the user module's decision and not this one's; with no
	// Provisioner, `provision` refuses as `existing` does.
	Provisioner contracts.Provisioner

	// Messages is the application's whole catalogue of copy, this module's
	// included, and it is what lets the passkey doors refuse a person in the
	// language they asked to be answered in: a browser ceremony is read on the
	// sign-in page, and that page's own words are translated. The reference
	// composition wires the application's own Skin copy — the one catalogue the
	// sentence names, merged from every layer that ships words — in
	// modules/auth/provider.go; a module that read its own messages file could
	// not answer in a language a UI catalogue carries. With none, every refusal
	// this module answers is English, which is what it was before the field
	// existed.
	Messages locale.Messages

	// LinkPages names the pages the set-password and confirmation links open on
	// the tenant's host. Zero is the admin shell's workspace screens, which is what
	// every composition mailed before the field existed. See LinkPages.
	LinkPages LinkPages

	// PublicHost is the name the application believes it is reached at. One
	// thing is decided from it: whether the session cookie is marked Secure. A
	// browser refuses a Secure cookie over http://localhost, so a development
	// machine would be a development machine nobody could sign in to.
	PublicHost string

	// Pages is the chrome of the page the verification link opens. Only the
	// emailed-verification lifecycle has such a page, so only that lifecycle
	// reads it; see Pages.
	Pages Pages
}

// LinkPages names the pages a mailed link opens, as paths on the host the tenant
// is served at: SetPassword for an invitation or a reset, ConfirmEmail for a
// sign-up's confirmation. The page reads the token from the link's query and
// calls the API door that exists for it (/api/v1/auth/password/reset,
// /api/v1/public/auth/verify-email); this module serves no page at either.
//
// Each is an absolute path with no query or fragment and never under the API
// root, and New panics on anything else. Empty is the workspace screen the admin
// shell mounts, /app/auth/reset or /app/auth/verify-email, so either may be named
// alone.
type LinkPages struct {
	SetPassword  string
	ConfirmEmail string
}

// check answers why a named page is one a mailed link cannot open: it is not a
// path on this site, it carries the query or fragment the appended token would
// collide with, or it is under the API root. Nil when both are empty or usable.
func (p LinkPages) check() error {
	for _, page := range []string{p.SetPassword, p.ConfirmEmail} {
		switch {
		case page == "":
		case !httpx.LocalPath(page) || strings.ContainsAny(page, "?#"):
			return fmt.Errorf("auth: a mailed link cannot open %q: name an absolute path on the tenant's host, with no query or fragment", page)
		case httpx.UnderAPI(page):
			return fmt.Errorf("auth: a mailed link cannot open %q: it is under the API root, and a link opens a page that calls the API", page)
		}
	}
	return nil
}

// NewPasskeyDoor returns the one read of a tenant's usernameless door, as
// contracts.PasskeyDoor (contracts/passkeys.go says why that is a second interface
// and not a method on Passkeys).
//
// It is a second value and not a second return of New because New is what every
// composition already calls and a door it may not have is a field nothing reads.
// The answer comes from the same row, by the same method, that BeginPasskeySignIn
// and FinishPasskeyAssertion act on, so a screen that asked here cannot be shown
// a door the ceremony then refuses.
func NewPasskeyDoor() contracts.PasskeyDoor { return internal.NewPasskeyDoor() }

// Module is the manifest, and the service it is built on: main hands the same
// value to kit/app as the authorizer and the identity hook.
//
// Deprecated: use NewChecked, which answers this refusal as an error.
func New(deps Deps) (contracts.Auth, module.Module) {
	svc, manifest, err := NewChecked(deps)
	if err != nil {
		panic(err.Error())
	}
	return svc, manifest
}

// NewChecked builds the service and the manifest over the ports the composition
// collected, and answers as an error the wiring that cannot hold: two
// registration lifecycles at once, a policy that names no roles or no registrar,
// a mailed link page that is not an absolute path outside the API root, a
// request budget the counter cannot keep, and an authorizer without its reader.
// It opens nothing, runs no query and is safe to call twice.
func NewChecked(deps Deps) (contracts.Auth, module.Module, error) {
	if deps.EmailRegistration != nil {
		if deps.Registration != nil || deps.ApprovalRegistration != nil {
			return nil, module.Module{}, errors.New("auth: choose only one registration lifecycle")
		}
		policy, err := deps.EmailRegistration.Checked()
		if err != nil {
			return nil, module.Module{}, err
		}
		deps.EmailRegistration = &policy
	}
	if deps.ApprovalRegistration != nil {
		if deps.Registration != nil {
			return nil, module.Module{}, errors.New("auth: choose emailed password setup or approval-required registration")
		}
		policy, err := deps.ApprovalRegistration.Checked()
		if err != nil {
			return nil, module.Module{}, err
		}
		deps.ApprovalRegistration = &policy
	}
	if err := deps.LinkPages.check(); err != nil {
		return nil, module.Module{}, err
	}
	if err := deps.AccountRequests.Check(); err != nil {
		return nil, module.Module{}, fmt.Errorf("auth: Deps.AccountRequests: %w", err)
	}
	deps, err := withGrants(deps)
	if err != nil {
		return nil, module.Module{}, err
	}
	secure := !config.Local(deps.PublicHost)
	svc := internal.NewServiceWithGrants(deps.Users, deps.Notify, internal.Delivery{
		Mailer: deps.Mailer, Hosts: deps.Hosts, Secure: secure, Mails: deps.Mails,
		MailDelivery:    deps.MailDelivery,
		SetPasswordPage: deps.LinkPages.SetPassword, ConfirmEmailPage: deps.LinkPages.ConfirmEmail,
		AccountRequests: deps.AccountRequests,
	}, deps.Authorization, deps.Permissions)
	cookies := internal.NewCookies(secure)
	manifest := module.Module{
		Name:       "auth",
		Views:      contracts.Views(deps.EmailRegistration != nil),
		Migrations: Migrations.Files,
		Adopts:     Migrations.Adopts,
		RulesFrom:  Migrations.RulesFrom,
		// Everything that proves a person acted. `first_factor_proofs.user_id` is a
		// primary key with no foreign key, which is why it is declared rather than
		// inferred from a referential edge that was never written.
		PersonalData: personaldata.Implements(
			personaldata.Table{Name: "sessions", Person: "user_id", Text: []string{"user_agent", "ip"}},
			personaldata.Table{Name: "password_tokens", Person: "user_id"},
			personaldata.Table{Name: "verification_tokens", Person: "user_id", Text: []string{"email"}},
			personaldata.Table{Name: "totp_factors", Person: "user_id"},
			personaldata.Table{Name: "recovery_codes", Person: "user_id"},
			personaldata.Table{Name: "api_tokens", Person: "user_id", About: []string{"created_by"}, Text: []string{"name"}},
			personaldata.Table{Name: "first_factor_proofs", Person: "user_id"},
			// A passkey is a person's own credential: the challenge row is about the
			// account being signed into, and the credential row is the person's key.
			personaldata.Table{Name: "passkey_challenges", Person: "user_id"},
			personaldata.Table{Name: "passkey_credentials", Person: "user_id"},
			// Which link this module decided to send, to which address. The row is a
			// credential event rather than something the person did, so it is erased
			// outright and not scrubbed.
			personaldata.Table{Name: "auth_mail_dispatch", Person: "user_id", Text: []string{"recipient"}},
		),
		Permissions: permissions,
		Declared:    contracts.Events,
		// Admin owns the role screen; personal session navigation belongs to the app.
		Nav: []module.NavEntry{
			{Label: "Roles", Screen: "auth/roles", Permission: contracts.PermissionRoleManage},
		},
		Jobs:          []jobs.Job{internal.Sweep(svc, deps.Tenants)},
		Subscriptions: sessionSubscriptions(svc),
		Routes:        sessionRoutes(svc, deps, cookies, secure)}
	if deps.Registration != nil {
		manifest.Subscriptions = append(manifest.Subscriptions, internal.RegistrationSubscription(svc, deps.Registration))
	}
	if deps.EmailRegistration != nil {
		manifest.Subscriptions = append(manifest.Subscriptions, internal.VerificationSubscriptions(svc)...)
	}
	return svc, manifest, nil
}

// sessionSubscriptions is the two ways the same fact arrives: somebody who cannot
// sign in has to be sent a link they can use. A send a transport refused is
// acknowledged rather than retried, in internal.AcknowledgeRefusedDelivery.
func sessionSubscriptions(svc *internal.Service) []events.Subscription {
	return []events.Subscription{{
		Module: "auth", Name: usercontracts.EventInvited,
		Handler: internal.AcknowledgeRefusedDelivery(func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			var invited usercontracts.Invited
			if err := json.Unmarshal(ev.Payload, &invited); err != nil {
				return fmt.Errorf("auth: read the invitation: %w", err)
			}
			return svc.Offer(internal.WithOrigin(internal.WithServed(ctx, invited.Served), ev), tx, invited.UserID)
		}),
	}, {
		Module: "auth", Name: contracts.EventResetRequested,
		Handler: internal.AcknowledgeRefusedDelivery(func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			var asked contracts.ResetRequested
			if err := json.Unmarshal(ev.Payload, &asked); err != nil {
				return fmt.Errorf("auth: read the reset request: %w", err)
			}
			return svc.Reissue(internal.WithOrigin(internal.WithServed(ctx, asked.Served), ev), tx, asked.Email)
		}),
	}}
}

func sessionRoutes(svc *internal.Service, deps Deps, cookies internal.Cookies, secure bool) func(httpx.Surfaces) {
	return func(s httpx.Surfaces) {
		svc.Declare(s.Permissions())
		internal.RegisterRoutes(s, internal.RouteServices{Login: svc, Logout: svc, Identity: svc, ChangePassword: svc, ForgotPassword: svc, ResetPassword: svc, ListSessions: svc, RevokeSession: svc, RevokeAllSessions: svc, ListRoles: svc, SetRole: svc}, cookies)
		internal.RegisterTokenRoutes(s, svc)
		svc.EnableFactors([]byte(deps.FactorKey))
		internal.RegisterFactorRoutes(s, svc, cookies)
		internal.RegisterPasskeyRoutes(s, svc, cookies, internal.PasskeyWords{Messages: deps.Messages})
		if deps.Registration != nil {
			internal.RegisterRegistrationRoutes(s, svc)
		}
		if deps.ApprovalRegistration != nil {
			internal.RegisterApprovalRegistrationRoutes(s, svc, *deps.ApprovalRegistration)
		}
		if deps.EmailRegistration != nil {
			internal.RegisterEmailRegistrationRoutes(s, svc, *deps.EmailRegistration)
			internal.MountVerifyEmailPage(s, svc, *deps.EmailRegistration, deps.Pages)
		}
		if deps.OIDC.Issuer != "" || deps.OIDCProviders != nil {
			internal.RegisterOIDCRoutes(s, svc, deps.Users, deps.Provisioner,
				internal.NewProvider(deps.OIDC, cookies, secure, deps.OIDCProviders, deps.Secrets))
		}
		if deps.SAMLProviders != nil {
			internal.RegisterSAMLRoutes(s, svc, deps.Users, deps.Provisioner,
				internal.NewSAMLProvider(cookies, secure, deps.SAMLProviders))
		}
	}
}

// permissions is what the manifest declares: two, guarding the two roles routes
// and the one route that opens the usernameless door. Every other route here is
// about the caller themselves. See contracts/permissions.go.
var permissions = []module.Permission{
	{Key: contracts.PermissionRoleManage, Label: "manage roles"},
	{Key: contracts.PermissionPasskeySignIn, Label: "let a passkey sign people in alone"},
}

// SeedRoles provisions a newly created tenant through auth's own storage path,
// independently of sessions, delivery and periodic jobs. Existing role grants
// are preserved on repeated calls.
//
// declared is the application's permission catalogue, which it gets from
// kit/module.Grants over the modules it composed — never a list written out
// here or at the call site. That is the whole point of the parameter: the
// operator grants the administrator's role is created holding are the operator
// permissions of the composed modules, so an application that does not compose
// the module owning a permission cannot seed it, and an initial role naming one
// no module defines is refused here instead of warned about on the hour.
// contracts.SeededRoles is the same decision without a database, for an
// application that wants it refused before it opens one.
func SeedRoles(ctx context.Context, tx db.Tx[db.System], tenant tenancy.Tenant, declared []tenancy.Grant, defaults []contracts.Role) error {
	return internal.SeedRoles(ctx, tx, tenant, declared, defaults)
}
