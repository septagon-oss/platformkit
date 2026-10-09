// Package auth is signing in: sessions, passwords, single sign-on, and the
// roles that decide what a caller may do.
//
// The application constructs tenants with SeedRoles before wiring notification
// delivery and authentication. Role provisioning needs no running auth service.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
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
	// sent. It never carries the link: the notice points at /auth/reset and the
	// secret is in the mail and nowhere else. A composition that wires none
	// still sends the mail.
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

	// Tenants is how the hourly sweep reaches every tenant, to delete the
	// sessions and tokens that have expired.
	Tenants jobs.TenantLister

	// OIDC is the installation's own identity provider, and the fallback for a
	// tenant that names none. An empty issuer means there is no default, and
	// then the two OIDC routes are mounted only when OIDCProviders is wired —
	// which is the case that lets two tenants sign in at two issuers in one
	// process, each answering for its own host.
	OIDC OIDC

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

// Module is the manifest, and the service it is built on: main hands the same
// value to kit/app as the authorizer and the identity hook.
func New(deps Deps) (contracts.Auth, module.Module) {
	if deps.EmailRegistration != nil {
		if deps.Registration != nil || deps.ApprovalRegistration != nil {
			panic("auth: choose only one registration lifecycle")
		}
		policy, err := deps.EmailRegistration.Checked()
		if err != nil {
			panic(err)
		}
		deps.EmailRegistration = &policy
	}
	if deps.ApprovalRegistration != nil {
		if deps.Registration != nil {
			panic("auth: choose emailed password setup or approval-required registration")
		}
		policy, err := deps.ApprovalRegistration.Checked()
		if err != nil {
			panic(err)
		}
		deps.ApprovalRegistration = &policy
	}
	secure := !config.Local(deps.PublicHost)
	svc := internal.NewService(deps.Users, deps.Notify, internal.Delivery{
		Mailer: deps.Mailer, Hosts: deps.Hosts, Secure: secure,
	})
	cookies := internal.NewCookies(secure)
	manifest := module.Module{
		Name:        "auth",
		Migrations:  Migrations.Files,
		Adopts:      Migrations.Adopts,
		RulesFrom:   Migrations.RulesFrom,
		Permissions: permissions,
		Declared:    contracts.Events,
		// The one entry, and the shell serves it: a role is keyed by its name
		// rather than by an id, so no generated screen could answer this path
		// and modules/admin writes the page. The two routes below are what it
		// is a face for. See modules/admin/internal/roles.go.
		// One nav entry, and deliberately only one. The session list is the
		// caller's own — the routes reach it through the credential and no
		// permission — and kit/module.Validate refuses an entry that names no
		// permission, because a link everybody sees is still a decision somebody
		// has to own. Borrowing role:manage would be the wrong decision twice
		// over: it hides the screen from the members it is for and it would put
		// an administrator's own list under a permission they hold for other
		// people's rows. So the manifest says nothing here, and the product whose
		// navigation this is names the entry beside the permission it seeds.
		Nav: []module.NavEntry{
			{Label: "Roles", Screen: "auth/roles", Permission: contracts.PermissionRoleManage},
		},
		Jobs: []jobs.Job{internal.Sweep(svc, deps.Tenants)},
		// Two subscriptions, and they are the same fact from two directions:
		// somebody who cannot sign in has to be sent a link they can use.
		//
		// Both are subscriptions rather than calls inside the request, and for
		// two different reasons. The invitation one, because sending mail is
		// somebody else's machine and a request that waited on one would hold a
		// transaction open across it. The reset one, because the request may
		// not look the address up at all: a public route that took longer for
		// an address somebody has is an account enumeration oracle with a
		// stopwatch, so the lookup happens here, where nobody is timing it.
		Subscriptions: []events.Subscription{{
			Module: "auth", Name: usercontracts.EventInvited,
			Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
				var invited usercontracts.Invited
				if err := json.Unmarshal(ev.Payload, &invited); err != nil {
					return fmt.Errorf("auth: read the invitation: %w", err)
				}
				return svc.Offer(internal.WithServed(ctx, invited.Served), tx, invited.UserID)
			},
		}, {
			Module: "auth", Name: contracts.EventResetRequested,
			Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
				var asked contracts.ResetRequested
				if err := json.Unmarshal(ev.Payload, &asked); err != nil {
					return fmt.Errorf("auth: read the reset request: %w", err)
				}
				return svc.Reissue(internal.WithServed(ctx, asked.Served), tx, asked.Email)
			},
		}},
		Routes: func(s httpx.Surfaces) {
			// The catalogue, taken at the one moment the kernel has it and this
			// module is being wired. The hourly sweep reads it back to say which
			// roles name a permission nothing defines any more.
			svc.Declare(s.Permissions())
			internal.RegisterRoutes(s, svc, cookies)
			// A person's own keys, mounted always: unlike the factor routes
			// there is no secret this needs from the deployment — the token is
			// the secret, and it is hashed rather than sealed because it is
			// never read back.
			internal.RegisterTokenRoutes(s, svc)
			// The factor key is the gate, and it is a deployment's setting
			// rather than a fact about which other modules were composed: a
			// composition with the key mounts the routes, one without it does
			// not, and nothing here looks at a module list to decide.
			// The factor routes are mounted whether or not the deployment set a
			// key, and with none they answer 503 — the refusal the spec names,
			// and the reason they are not mounted conditionally: an operation
			// that exists only sometimes leaves its four events declared and
			// unreachable, which the application's own catalogue check (every
			// declared event has a channel) is right to call a lie. A deployment
			// without a key is told the door is shut rather than being handed a
			// 404 that says nothing about why.
			svc.EnableFactors([]byte(deps.FactorKey))
			internal.RegisterFactorRoutes(s, svc, cookies)
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
			// The gate is "can any tenant here reach a provider", which is the
			// installation's issuer or the port that resolves one per tenant —
			// not the installation's issuer alone, which would refuse to mount
			// the two legs for a deployment whose providers are all per-tenant.
			if deps.OIDC.Issuer != "" || deps.OIDCProviders != nil {
				internal.RegisterOIDCRoutes(s, svc, deps.Users, deps.Provisioner,
					internal.NewProvider(deps.OIDC, cookies, secure, deps.OIDCProviders, deps.Secrets))
			}
		},
	}
	if deps.Registration != nil {
		manifest.Subscriptions = append(manifest.Subscriptions, internal.RegistrationSubscription(svc, deps.Registration))
	}
	if deps.EmailRegistration != nil {
		manifest.Subscriptions = append(manifest.Subscriptions, internal.VerificationSubscriptions(svc)...)
	}
	return svc, manifest
}

// permissions is what the manifest declares: one, guarding the two roles
// routes. Every other route here is about the caller themselves. See
// contracts/permissions.go.
var permissions = []module.Permission{{Key: contracts.PermissionRoleManage, Label: "manage roles"}}

// SeedRoles provisions a newly created tenant through auth's own storage path,
// independently of sessions, delivery and periodic jobs. Pass trusted defaults
// checked against the composition with contracts.CheckedPermissions before
// bootstrap or startup. Existing role grants are preserved on repeated calls.
func SeedRoles(ctx context.Context, tx db.Tx[db.System], tenant tenancy.Tenant, operator []string, defaults []contracts.Role) error {
	return internal.SeedRoles(ctx, tx, tenant, operator, defaults)
}
