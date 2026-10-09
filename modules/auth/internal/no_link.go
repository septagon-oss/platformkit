package internal

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// The one message this module hands the transport when a request for a link could
// not be answered with a link. It is the same sentence whichever branch raised it —
// nobody has the address, the account cannot be sent a link, a link this person was
// already sent is still outstanding — because the sentence is addressed to whoever
// owns this mailbox and a different sentence per branch would be the account's
// status written into a mail. It names no address, no token and no link.
const (
	noLinkSubject = "We did not send a link to this address"
	noLinkBody    = "Somebody asked for a link to this address. No link was sent to it." +
		" If that was you, check the address you asked with and ask again." +
		" If it was not you, nothing needs to be done."
)

// noLink mails and records the message above, in the caller's transaction, for one
// of the two `*_no_link` kinds.
//
// # Why a call that sends no link sends a message at all
//
// The public delivery door (RegisterEmailRegistrationRoutes' auth-mail-delivery)
// answers one question — did the mail this call asked for leave? — and the only
// answer that is not a fact about the address is the one carried by a record this
// call caused through the transport. contracts.MailReport refuses to report
// anything but a refusal, precisely because "a mail left" is the shape of "somebody
// has this account"; the previous cure for that answered a caller whose call left no
// record with the tenant's newest row instead, and review 3 showed the same oracle
// through it: a transport that refuses one mailbox can be refilled by the caller, so
// every no-account probe was answered `failed` while every account was answered
// `pending`.
//
// What both counterexamples leave is one rule the door can hold: a caller is
// answered from a record its own call caused through the same transport, which means
// every branch of a call that names an address has to hand one message to that
// transport. That is what this function is. It costs one mail per request — never
// more than the link itself would have cost, because the branches are exclusive —
// and the request is already capped by the budget the route that raised it applies
// (contracts.ResetRequests, contracts.MailDeliveryAsks, the recipient cooldown).
//
// It carries no credential, so a refused send has no token row to put back and no
// secret to scrub out of the record's reason; recordMail still redacts it, and still
// publishes auth.mail_failed beside the row, because a mail that did not go is a
// fact the tenant's own trail should hold whichever message it was.
func (s *Service) noLink(ctx context.Context, tx db.Tx[db.Tenant], kind, recipient string) error {
	if s.mail.Mailer == nil {
		// The deployment's fault, not the caller's: a suppressed record, which is
		// the answer every other no-mail branch of this flow gives, so a
		// composition with no transport cannot be told apart from an address with
		// nobody behind it.
		return s.recordMail(ctx, tx, kind, recipient,
			notificationcontracts.MailSuppressed, "no mail transport is wired")
	}
	err := s.mail.Mailer.Send(ctx, notificationcontracts.Message{
		To: recipient, Subject: noLinkSubject, Body: noLinkBody,
	})
	if err != nil {
		return s.recordMail(ctx, tx, kind, recipient,
			notificationcontracts.MailFailed, err.Error())
	}
	return s.recordMail(ctx, tx, kind, recipient, notificationcontracts.MailSent, "")
}

// noResetLink and noVerificationLink are the two calls of that message, named for
// the link that was not sent so the kind in the row says which door the person was
// standing at.
func (s *Service) noResetLink(ctx context.Context, tx db.Tx[db.Tenant], recipient string) error {
	return s.noLink(ctx, tx, contracts.MailResetNoLink, recipient)
}

func (s *Service) noVerificationLink(ctx context.Context, tx db.Tx[db.Tenant], recipient string) error {
	return s.noLink(ctx, tx, contracts.MailVerificationNoLink, recipient)
}
