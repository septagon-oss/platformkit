package notification

import (
	"fmt"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/secrets"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
	gomail "github.com/septagon-oss/platformkit/modules/notification/providers/gomail"
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
	// The relay's credential. A module that read it from the environment itself
	// would know the deployment — which store a secret comes from is the
	// deployment's fact and never the module's — so this module names the need and
	// the composition answers it, exactly as it answers a storage port.
	pkit.Needs[secrets.Secrets](),
	// The device adapters this installation carries, if it carries any. Optional
	// because a deployment with no push credentials is the normal one today: with
	// nothing wired, the two device channels are suppressed with a reason and no
	// subscription is composed for them — the same reading of Deps.Providers that
	// decides whether a notice's push row is ever opened.
	pkit.Optional[push.Adapters](),
	pkit.Provides[contracts.Service](),
	// auth declares Notifier as its own port — the one command it has of
	// telling somebody something — and this module's Service is the answer to
	// it. It is put under auth's key rather than adapted in a composition file
	// because no adapter is needed: the two methods are the same method, and a
	// composition that wrote one would be a second caller of Notify with a
	// shorter name.
	pkit.Provides[authcontracts.Notifier](),
	pkit.Provides[contracts.Mailer](),
	// Whether that sender delivers anything, which is the same object's second
	// reading and not a second value: a composition may not hold a Mailer and a
	// delivery answer that contradict each other, because the two questions are
	// asked of the one sender.
	pkit.Provides[contracts.MailDelivery](),
	// The ledger of the mails a module sends without a notice behind it. auth
	// names this same type `authcontracts.MailLedger` — an alias, not a second
	// interface — so one Put answers both spellings, and the module that records
	// the attempt is the module that owns the record's table.
	pkit.Provides[contracts.MailLedger](),
	pkit.Contributes[personaldata.Part](),
	// The module's own screen. Which chrome a person's list of their own notices
	// sits in is an installation's fact — the same shape change's two review
	// screens take — and a deployment that hands in none has no list page to ask
	// for, exactly as it had none before this line existed.
	pkit.Optional[*Page](),
	pkit.FromDeployment(
		pkit.Implementation{Name: "smtp", Inputs: []string{"mail.host", "mail.port", "mail.from"}},
		pkit.Implementation{Name: "mailbox", Simulated: true},
		pkit.Implementation{Name: "none"},
	),
)

// mailSecret is the name this module's SMTP carrier reads its password under. The
// deployment's own, not a tenant's: a tenant that held an SMTP credential would name
// its own address here, and that decision belongs to the product.
const mailSecret = "mail.password"

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
		// The carrier is its own package so the SDK stays out of internal/ and
		// contracts/; this pick is the only thing in the module that reads it.
		mailer = gomail.New(gomail.Config{
			Host: mail.Host, Port: mail.Port, Username: mail.Username,
			// The name, not the bytes: the carrier reads the password off the port
			// when it dials, so a secret that arrives mid-incident is picked up by
			// the next attempt rather than needing a restart.
			Secrets: pkit.Get[secrets.Secrets](w), SecretName: mailSecret,
			EnvelopeFrom: mail.From,
		})
	case "mailbox":
		mailer = NewMailbox()
	default:
		// The implementation named "none": this deployment sends no mail. It is a
		// value rather than a nil so the port the module Provides always answers
		// (the composition tests refuse a Provided port
		// that answers with nothing), and the value says what it is: every send
		// answers ErrNoTransport, the delivery ledger records the notice as
		// suppressed, and the command that would promise a link in a mailbox
		// refuses on authcontracts.MailDeliverable before it writes anything.
		mailer = contracts.NoTransport
	}
	// The carrier answers for itself, and one object is put twice — the way site
	// puts its settings service under Service and LockedReader. A carrier that
	// answers nothing is a defect in this build and refuses the boot that would
	// otherwise let a deployment mail nobody and believe otherwise.
	delivery, ok := mailer.(contracts.MailDelivery)
	if !ok {
		return module.Module{}, fmt.Errorf("notification: the mail carrier %T does not answer whether it delivers", mailer)
	}
	svc, manifest := New(Deps{
		Recipients: pkit.Get[contracts.RecipientLookup](w),
		// Nil when this composition hands in no chrome, which is the module's
		// own answer to a screen nobody composed: no route, and the surface's 404.
		Page:   pkit.Get[*Page](w),
		Mailer: mailer,
		Hosts:  pkit.Get[contracts.HostLookup](w),
		// The same rule the session cookie's Secure flag follows, so there is
		// one answer to "is this deployment https" and one place it is written.
		Secure: !config.Local(server.PublicHost),
		// The zero map when nothing put it: no device channel is carried, and the
		// decision says so in the ledger rather than at send time.
		Push: pkit.Get[push.Adapters](w),
	})
	pkit.Put(w, svc)
	pkit.Put[authcontracts.Notifier](w, svc)
	pkit.Put[contracts.MailLedger](w, svc)
	pkit.Put[contracts.Mailer](w, mailer)
	pkit.Put[contracts.MailDelivery](w, delivery)
	pkit.Put[personaldata.Part](w, internal.PersonalData())
	return manifest, nil
}
