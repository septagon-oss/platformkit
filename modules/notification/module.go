// Package notification is the module manifest: telling somebody something, in
// the application and optionally by mail.
//
// It is the shape every module follows, with one deliberate omission: there is
// no rest.Spec. A Spec's list route is the whole tenant, and these rows are
// addressed to a person — every caller's list is a different list — so the two
// routes are written by hand and scoped by the principal rather than by a
// permission. See internal/handler.go.
package notification

import (
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

// Mail is one outgoing mail server. It has the same shape as config.Mail, so
// main converts one to the other in a line.
type Mail = internal.Mail

// SMTP is the production Mailer for a configured server. main wires it, or
// the in-memory Mailbox when there is none, so the choice is visible in the
// file that composes the application.
var SMTP = internal.NewSMTP

// Deps is what this module cannot make for itself.
type Deps struct {
	// Recipients turns a user id into an email address. The interface is
	// declared in this module's contracts/ and satisfied by an adapter over the
	// user module in apps/platformkit: the app adapts, so notification never
	// names user. A nil lookup writes every row and sends no mail.
	Recipients contracts.RecipientLookup

	// Senders is the tenant's own sending address — its name, its domain, its
	// DKIM pair and what proved them. The product builds it over its own
	// configuration, because what proves a domain (a TXT record read by a
	// resolver, an operator who looked, an installation with no egress at all) is
	// the product's fact and not this module's.
	//
	// Nil is the other answer and it is not "no mail": it means this installation
	// speaks as the address in its own configuration for every tenant it serves,
	// which is what it did before per-tenant senders existed, and the decision
	// says so rather than pretending the tenant chose it.
	Senders contracts.Senders

	// Providers are the carriers beyond mail — VAPID for a browser, T-0120's
	// adapter for a phone, an HTTP client for a tenant's endpoint. A channel with
	// no provider here is suppressed with a reason that names the deployment, so
	// the ledger accounts for every channel a notice asked for whatever this list
	// holds. Mail is not one of them: the module's own worker carries it, which is
	// what Deps.Mailer is for.
	Providers contracts.Providers

	// Mailer sends the rendered message, in the worker.
	Mailer contracts.Mailer

	// Hosts turns the tenant an event belongs to into the host its people reach
	// the application at, which is what a notice's link has to become for a mail
	// client. It is declared in this module's contracts/ and satisfied by an
	// adapter over the tenant module in apps/platformkit, the same way
	// Recipients is satisfied over the user module.
	Hosts contracts.HostLookup

	// Secure says the application is reached over https, which is the scheme a
	// mailed link is built with. It is one bool rather than a host because it is
	// the same decision the session cookie's Secure flag is: a laptop reached at
	// a local name gets http, and everything else gets https.
	Secure bool
}

// Module is the manifest, and the service it is built on: main holds the
// service because the modules that raise notices are wired against it.
//
// permissions is what the manifest declares, and it is empty. It is a var
// rather than a nil literal in the manifest so that all six modules answer the
// question in the same place and in the same shape: a reader looking for what a
// module lets a role be granted finds one name, whatever the answer is.
//
// The answer here is nothing. Both routes are about the caller themselves,
// which is what httpx.SignedIn is for, and a permission every signed-in person
// must hold is a permission that decides nothing.
var permissions []module.Permission

func Module(deps Deps) (contracts.Service, module.Module) {
	// A wiring mistake fails where it is written rather than as a nil
	// dereference in the worker an hour later.
	if deps.Mailer == nil {
		panic("notification.Module: Deps.Mailer is required; wire notification.NewMailbox() when there is no mail server")
	}
	svc := internal.NewService(deps.Recipients,
		internal.WithPreferences(internal.Prefs{}),
		internal.WithSenders(deps.Senders),
		internal.WithProviders(deps.Providers))
	return svc, module.Module{
		Name:        "notification",
		Migrations:  Migrations.Files,
		Adopts:      Migrations.Adopts,
		Permissions: permissions,
		Events:      contracts.Events,
		// No nav entry, and it is the same fact as the empty Permissions: a
		// nav entry names the permission that decides who sees the link, and
		// there is no permission here to name. Everybody's notifications are
		// their own, so the link belongs in the chrome the admin shell puts
		// around every page (E4) rather than in the module list.
		Nav: nil,
		// No periodic work: a notification is caused by something happening,
		// which is an event and not the clock (docs/adr/0004).
		Jobs:          nil,
		Subscriptions: []events.Subscription{internal.SendMail(deps.Mailer, deps.Recipients, deps.Hosts, deps.Secure)},
		Routes:        func(s httpx.Surfaces) { internal.RegisterRoutes(s.App, svc) },
	}
}

// Settings is this module's own answer about who may be told what: the channel
// switches, the opt-out and the quiet window, as contracts.PreferenceService and
// contracts.Preferences over the module's own two tables. It needs nothing from
// the composition, because the rows are the module's and every read runs in the
// caller's transaction under the tenant that transaction names.
//
// The screens and routes that reach it are the product's share of this brief:
// every command here is scoped to the principal whose id it is handed, so the
// page is a form over one's own rows and there is no shape of it that reaches
// another person's choices.
func Settings() contracts.PreferenceService { return internal.Prefs{} }

// Senders is the tenant's own sending address as a contracts.Senders and a
// contracts.SenderAdmin: the module keeps the row, the token, the refusal and the
// audit, and the two collaborators below are what it cannot be the authority
// about — whether a domain says so, and where the key that signs for it lives.
// Either may be nil, and each nil is a named refusal rather than a surprise: a
// deployment with no verifier cannot verify anything, and a deployment with no
// key suppresses the mail it could not sign.
func Senders(verifier contracts.SenderVerifier, keys contracts.DKIMKeys) (contracts.Senders, contracts.SenderAdmin) {
	s := &internal.Senders{Verifier: verifier, Keys: keys}
	return s, s
}
