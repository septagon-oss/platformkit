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

	// Mailer sends the rendered message, in the worker.
	//
	// nil means this installation has no mail transport at all: the deployment
	// picked notification.Module's "none" because the configuration names no
	// server and did not ask for the in-memory sink either. It is a value rather
	// than a panic because "no mail server here" is a state a real deployment is
	// in, and the module's job in that state is to say so on the record: every
	// notice is still written and still read in the application, and the mail each
	// one asks for is recorded as suppressed with the reason, which is the delivery
	// ledger's own vocabulary rather than a log line somebody has to notice. What
	// refuses outright is a command whose promise is the message itself
	// (modules/auth's emailed verification link): that one answers a reasoned 503
	// and writes nothing, because there is no ledger a stranger can read.
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

func New(deps Deps) (contracts.Service, module.Module) {
	svc := internal.NewService(deps.Recipients)
	return svc, module.Module{
		Name:        "notification",
		Migrations:  Migrations.Files,
		Adopts:      Migrations.Adopts,
		Permissions: permissions,
		Declared:    contracts.Events,
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
