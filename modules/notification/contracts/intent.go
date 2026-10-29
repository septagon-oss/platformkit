package contracts

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

// The six states of a mail this module owes (migrations/000076). They are the
// column's CHECK spelled in Go, the way webhook/contracts/delivery.go spells its
// five: a worker that could be handed a state the table refuses would be a state
// write that fails at the database instead of at the caller.
//
//	queued      — owed, not yet handed to a transport
//	in_flight   — claimed; a socket is open or a process died holding one
//	sent        — the relay took it (terminal)
//	failed      — the relay refused it, or it waited past MailIntentWindow (terminal)
//	suppressed  — nothing could carry it: no transport, no sender, an address that
//	              moved, a notice that was deleted (terminal)
//	unknown     — the answer never came; the relay may have taken it
const (
	MailIntentQueued     = "queued"
	MailIntentInFlight   = "in_flight"
	MailIntentSent       = "sent"
	MailIntentFailed     = "failed"
	MailIntentSuppressed = "suppressed"
	MailIntentUnknown    = "unknown"
)

// The module's own numbers about a mail that does not go, all in one place so the
// README, a test and the worker read one name. They are constants and not Deps
// fields because they are the module's promise: a deployment that tuned the number
// of times a person is emailed the same thing twice would be a deployment editing
// this module's promise rather than its own configuration.
const (
	// MailIntentMaxAttempts is how many times one intent may be handed to a
	// transport for the answers a relay provably gave — it took it, or it refused
	// it. An ambiguous answer is not one of these; see MailIntentMaxUnknown.
	MailIntentMaxAttempts = 3

	// MailIntentWindow is how long a queued mail stays a mail. A notice mailed two
	// days after the event is not a notice — the person still has the in-app row —
	// so the row ends failed rather than leaving a queue for a late relay.
	MailIntentWindow = 24 * time.Hour

	// MailIntentUnknownDelay is how long an ambiguous send waits before the second
	// one, and MailIntentMaxUnknown how many second chances an ambiguous send gets.
	//
	// SMTP has no idempotency key and no deduplication window to outlast, so the
	// two numbers price a trade rather than eliminating it: the cost of the re-send
	// is a second copy of one message, the cost of refusing it is a mail that
	// silently never arrives. At a day's distance a second copy reads as a reminder,
	// and one re-send bounds what a relay that drops every connection mid-DATA can
	// produce. An intent that has spent them stays `unknown` — visible forever,
	// never claimed again — because a row that says "we do not know" is the record
	// this module keeps, and a state that quietly became `sent` would be a lie.
	MailIntentUnknownDelay = 24 * time.Hour
	MailIntentMaxUnknown   = 1

	// MailIntentRetentionDays is how long a finished intent is kept. An unfinished
	// one is never forgotten: see the sweep's own rule in internal/mail_retention.go.
	MailIntentRetentionDays = 90

	// MaxMailIntentBytes bounds one stored message. The carrier refuses a mail with
	// no text body; this bounds the three parts it does accept, because a queue
	// row that holds megabytes is a queue that cannot be read.
	MaxMailIntentBytes = 256 * 1024

	// maxMailIntentCause bounds the caller's stable key. It is the reason-column
	// bound of 000044, which is the width this module already holds a caller's
	// words at.
	maxMailIntentCause = 200
)

// OutgoingMail is the mail a caller decides to send, before anything is sent: the
// three facts that make it deduplicable and addressable, and the one frozen shape
// that carries what it says.
//
// Message is reused rather than restated. It is the rendered mail as every
// Mailer already accepts it, so the recipient and the body this port freezes are
// one type whose shape this module has already promised not to reshape.
type OutgoingMail struct {
	// Purpose is which mail this is, in the grammar every event name uses —
	// "notification.email_requested", "billing.invoice_due". The caller names its
	// own; no shared module holds a list of them.
	Purpose string
	// Cause is the stable identity of whatever asked for this mail: the source
	// event's id when a mail was caused by an event, or a key the caller owns and
	// keeps across its own retries. Two Queues of one (Purpose, Cause) are one
	// intent — the database's fact, not a check-then-insert. Empty claims no
	// identity, and every such mail is its own intent.
	Cause string
	// Person is who the mail is about, uuid.Nil when no person in this tenant is
	// nameable. It is what an export or an erasure reaches, and what the worker
	// re-reads the address from.
	Person uuid.UUID
	// Notice is the notification whose channel this mail is, uuid.Nil for a mail
	// with no notice behind it. A notice that is gone by the time the mail is due
	// ends the mail suppressed.
	Notice uuid.UUID
	// Message is the rendered mail: Message.To is the address the mail was decided
	// for, and Message.Sender is not stored — the worker reads the tenant's sender
	// at the moment it sends, so a key never sits in a queue.
	Message Message
}

// MailIntent is one mail this module owes, as a caller or an operator reads it.
type MailIntent struct {
	// ID is the delivery id. Seq is the append order: created_at alone cannot
	// order the rows one transaction writes.
	ID  uuid.UUID
	Seq int64
	// Purpose, Cause, Person and Notice mean what OutgoingMail's fields mean.
	Purpose string
	Cause   string
	Person  uuid.UUID
	Notice  uuid.UUID
	// Recipient is the address the mail was decided for, spelled by the caller
	// through contracts.EmailKey. Nothing here re-normalises it a second time.
	Recipient string
	// Subject, Body, HTML, Lang and ReplyTo are the frozen message. Sender is
	// deliberately absent: a DKIM key in a queue row is a key in a table.
	Subject string
	Body    string
	HTML    string
	Lang    string
	ReplyTo string
	// State is one of the six above. Attempt counts the transports this intent was
	// handed, so a crash cannot inflate it without a socket opening: only a claim
	// raises it. Uncertain counts the hand-offs whose answer never came.
	State     string
	Attempt   int
	Uncertain int
	// AvailableAt is when a queued or re-armable unknown row may be sent.
	// AttemptedAt is when this attempt was claimed — the lease's origin, and the
	// only timestamp that can bound "crashed across the socket".
	AvailableAt time.Time
	AttemptedAt *time.Time
	// Reason is the last answer, redacted and clipped: empty with MailIntentSent,
	// always present with a state that says the mail did not go.
	Reason string
	// CreatedAt and UpdatedAt are the database's own, as both delivery ledgers'
	// are: row order and the log's own clock, never a fake one's.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MailIntents is the durable half of sending mail: the decision, written where a
// crash can find it, and the send that happens after the decision commits.
//
// It is a capability beside Mailer, MailDelivery and MailLedger and not a method on
// any of them: Mailer.Send keeps sending immediately for whoever composes it, and a
// mail that must survive a rollback needs a transaction rather than a socket.
//
// It is not the door a credential-bearing message goes through. The message an
// intent holds is stored verbatim in the tenant's database, which is exactly what
// makes sending it after the commit possible — and exactly why a mail whose body
// is a live secret (a set-password link) does not come this way. That refusal is
// a boundary on who may call, not a validation: no check can tell a token from a
// sentence, and a scrub that claimed to would be a scrub nobody should trust.
type MailIntents interface {
	// Queue writes the intent in the caller's transaction and never opens one of
	// its own: a duty that outlived the decision that owed it would be a duty no
	// one asked for, and a rolled-back decision must owe nothing. It takes no
	// actor and asks no permission, for the reason RecordMail gives — the caller's
	// own command rechecked the actor, and row level security bounds the tenant
	// whose rows are touched, so a foreign Notice or Person resolves to no row.
	//
	// It returns the intent as written, which for a repeat of a (Purpose, Cause)
	// that already owes a mail is the intent that was already owed, and never the
	// caller's input re-read back at it. The second result says whether this call
	// appended that row: false is a redelivered event acknowledging a mail it did
	// not add. It is refused by contracts.PrepareIntent for a mail the table's own
	// CHECKs would refuse, so a caller fails in its own test rather than against a
	// database.
	Queue(ctx context.Context, tx db.Tx[db.Tenant], mail OutgoingMail) (*MailIntent, bool, error)
}

// The two answers a transport can give that no error code carries, and which the
// intent's whole rule turns on. A carrier refines towards the first — it names
// the reason it knows the message provably never reached a relay — and defaults
// to the second, because a sender that guesses "not sent" and is wrong loses a
// mail quietly, and a sender that guesses "sent" and is wrong sends a person the
// same message twice.
var (
	// ErrNotAccepted says the socket never carried the message: the dial was
	// refused, DNS or TLS failed, the message was refused before the wire. Nothing
	// was sent, so an attempt at it can be made again on the ladder rather than
	// waited out.
	ErrNotAccepted = errors.New("notification: the message never reached a mail server")

	// ErrUncertainSend says the answer never came back. The relay may have taken
	// the message, so the intent becomes unknown and the rule behind
	// MailIntentMaxUnknown decides — no state that says what happened is available
	// to write here, and writing one anyway is what this error exists to prevent.
	ErrUncertainSend = errors.New("notification: the mail server never answered")
)

// The four refusals PrepareIntent makes that PrepareMail's do not cover, one error
// each: every one is a caller bug that a different input fixes, and a caller that
// compared error text would be a caller that can be reworded out of its own check.
// An address that is not one is refused by ErrMailRecipient, the same error the mail
// ledger refuses it with — the rule is the same rule.
var (
	ErrMailPurpose = errors.New("notification: a mail intent's purpose is a name like notification.email_requested")
	ErrMailCause   = errors.New("notification: a mail intent's cause is the id or key of what asked for it")
	ErrMailMessage = errors.New("notification: a mail has a text body")
	ErrMailSize    = errors.New("notification: a mail is too large to be held as an intent")
)

// PrepareIntent applies the table's own rules to a mail and returns what 000076
// would refuse, so the fake and the Postgres service refuse the same input: a
// purpose that is not a name, a cause over the bound, no address or an
// over-long one, a message with no text part, a message over MaxMailIntentBytes.
// The recipient comes back trimmed, and Message.Sender comes back nil — the
// worker reads the tenant's sender when it sends, so nothing here stores one.
func PrepareIntent(mail OutgoingMail) (OutgoingMail, error) {
	msg := mail.Message
	msg.Sender = nil
	msg.To = strings.TrimSpace(msg.To)
	switch {
	case !transport.ValidName(mail.Purpose):
		return mail, ErrMailPurpose
	case len(mail.Cause) > maxMailIntentCause:
		return mail, ErrMailCause
	case msg.To == "" || len(msg.To) > maxMailRecipient:
		mail.Message = msg
		return mail, ErrMailRecipient
	case msg.Body == "":
		mail.Message = msg
		return mail, ErrMailMessage
	case len(msg.Subject)+len(msg.Body)+len(msg.HTML) > MaxMailIntentBytes:
		mail.Message = msg
		return mail, ErrMailSize
	}
	mail.Message = msg
	return mail, nil
}
