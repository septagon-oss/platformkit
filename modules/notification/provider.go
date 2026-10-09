package notification

import (
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/module"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the notice board as the resolver sees it, for an application that
// names it in Use: `Use(notification.Module)`.
//
// Two contracts come out. Service is what every module that raises a notice
// takes. Mailer is the same sender the worker delivers with, put so that the one
// module which has to put a secret in a message without it becoming a row first —
// auth, for a set-password link — reads the deployment's answer rather than
// building a second one: two senders in one process is two answers to "where does
// this installation's mail go".
//
// Which sender that is is the deployment's pick (FromDeployment), and there are
// three answers rather than two: smtp when the process names a host, port and
// from-address; the in-process mailbox when the deployment is a development one
// and no server was named; and no transport at all when a production deployment
// names neither.
//
// The mailbox is the resolver's `Simulated` implementation, which is the whole of
// the difference between the two mail-less states. A sink wired because nothing
// else was configured is how an installation ends up accepting a sign-up whose
// confirmation link nobody will ever read, and that is why the mailbox is refused
// outside development (pkit refuses a simulated implementation there) rather than
// offered as a default: a development machine that names no relay gets a mailbox
// it can read a link out of, and a production installation that names no relay
// gets the sender that refuses every send. With that sender the command that
// would promise a mailed link answers a reasoned refusal and writes no row
// (modules/auth refuses on exactly this value), and a notice that asks for mail is
// recorded as suppressed on the delivery ledger
// (modules/notification/internal/mail.go), which is an answer a reader can act on
// rather than a message that silently never arrives.
var Module = pkit.NewModule("notification", wire,
	pkit.Needs[contracts.RecipientLookup](),
	pkit.Needs[contracts.HostLookup](),
	pkit.Provides[contracts.Service](),
	// auth declares Notifier as its own port — the one command it has of
	// telling somebody something — and this module's Service is the answer to
	// it. It is put under auth's key rather than adapted in a composition file
	// because no adapter is needed: the two methods are the same method, and a
	// composition that wrote one would be a second caller of Notify with a
	// shorter name.
	pkit.Provides[authcontracts.Notifier](),
	pkit.Provides[contracts.Mailer](),
	pkit.FromDeployment(
		pkit.Implementation{Name: "smtp", Inputs: []string{"mail.host", "mail.port", "mail.from"}},
		pkit.Implementation{Name: "mailbox", Simulated: true},
		pkit.Implementation{Name: "none"},
	),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	mail := pkit.Config(w, func(c config.Config) config.Mail { return c.Mail })
	server := pkit.Config(w, func(c config.Config) config.Server { return c.Server })
	// The three inputs the smtp pick reads are this struct's three host, port and
	// from fields, so an implementation the deployment could pick is always one
	// this line can build. The mailbox reads nothing — a development deployment
	// that named no server named nothing else either — and "none" reads nothing
	// and builds the sender that refuses.
	var mailer contracts.Mailer
	switch w.Implementation() {
	case "smtp":
		mailer = SMTP(Mail{
			Host: mail.Host, Port: mail.Port, Username: mail.Username,
			Password: mail.Password, From: mail.From,
		})
	case "mailbox":
		mailer = NewMailbox()
	default:
		// The implementation named "none": this deployment sends no mail. It is a
		// value rather than a nil so the port the module Provides always answers
		// (apps/platformkit/composition_wiring_test.go refuses a Provided port
		// that answers with nothing), and the value says what it is: every send
		// answers ErrNoTransport, the delivery ledger records the notice as
		// suppressed, and the command that would promise a link in a mailbox
		// refuses on authcontracts.MailDeliverable before it writes anything.
		mailer = contracts.NoTransport
	}
	svc, manifest := New(Deps{
		Recipients: pkit.Get[contracts.RecipientLookup](w),
		Mailer:     mailer,
		Hosts:      pkit.Get[contracts.HostLookup](w),
		// The same rule the session cookie's Secure flag follows, so there is
		// one answer to "is this deployment https" and one place it is written.
		Secure: !config.Local(server.PublicHost),
	})
	pkit.Put(w, svc)
	pkit.Put[authcontracts.Notifier](w, svc)
	pkit.Put[contracts.Mailer](w, mailer)
	return manifest, nil
}
