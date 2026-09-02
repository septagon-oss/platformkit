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
// notificationtest.Mailbox when there is none, so the choice is visible in the
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
	Mailer contracts.Mailer

	// PublicHost is the name the application is reached at. One thing is
	// decided from it: the absolute URL a notice's link becomes in an email,
	// because a mail client has no base to resolve a path against.
	PublicHost string
}

// Module is the manifest, and the service it is built on: main holds the
// service because the modules that raise notices are wired against it.
//
// This module declares no permissions. Both routes are about the caller
// themselves, which is what httpx.SignedIn is for, and a permission that every
// signed-in person must hold is a permission that decides nothing.
func Module(deps Deps) (contracts.Service, module.Module) {
	// A wiring mistake fails where it is written rather than as a nil
	// dereference in the worker an hour later.
	if deps.Mailer == nil {
		panic("notification.Module: Deps.Mailer is required; wire notificationtest.NewMailbox() when there is no mail server")
	}
	svc := internal.NewService(deps.Recipients)
	return svc, module.Module{
		Name:        "notification",
		Permissions: nil,
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
		Subscriptions: []events.Subscription{internal.SendMail(deps.Mailer, deps.PublicHost)},
		Routes:        func(api *httpx.API) { internal.RegisterRoutes(api, svc) },
	}
}
