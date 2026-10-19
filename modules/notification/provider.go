package notification

import (
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/push"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
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
// Which sender that is is the deployment's pick (FromDeployment): smtp when the
// process names a host, port and from-address, the in-memory mailbox otherwise.
// Neither is simulated, and that is not an oversight — the mailbox is what a
// deployment with no SMTP actually runs, it keeps every message and logs each
// one, and marking it simulated would refuse every mail-less production
// installation at boot.
var Module = pkit.NewModule("notification", wire,
	pkit.Needs[contracts.RecipientLookup](),
	pkit.Needs[contracts.HostLookup](),
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
	// The ledger of the mails a module sends without a notice behind it. auth
	// names this same type `authcontracts.MailLedger` — an alias, not a second
	// interface — so one Put answers both spellings, and the module that records
	// the attempt is the module that owns the record's table.
	pkit.Provides[contracts.MailLedger](),
	// The module's own screen. Which chrome a person's list of their own notices
	// sits in is an installation's fact — the same shape change's two review
	// screens take — and a deployment that hands in none has no list page to ask
	// for, exactly as it had none before this line existed.
	pkit.Optional[*Page](),
	pkit.FromDeployment(
		pkit.Implementation{Name: "smtp", Inputs: []string{"mail.host", "mail.port", "mail.from"}},
		pkit.Implementation{Name: "mailbox"},
	),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	mail := pkit.Config(w, func(c config.Config) config.Mail { return c.Mail })
	server := pkit.Config(w, func(c config.Config) config.Server { return c.Server })
	// The three inputs the pick reads are this struct's three host, port and
	// from fields, so an implementation the deployment could pick is always one
	// this line can build.
	var mailer contracts.Mailer
	if w.Implementation() == "smtp" {
		// The carrier is its own package so the SDK stays out of internal/ and
		// contracts/; this pick is the only thing in the module that reads it.
		mailer = gomail.New(gomail.Config{
			Host: mail.Host, Port: mail.Port, Username: mail.Username,
			Password: mail.Password, EnvelopeFrom: mail.From,
		})
	} else {
		mailer = NewMailbox()
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
	return manifest, nil
}
