package contracts

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

// The three outcomes a mail that left this process can have. `requested` is
// deliberately absent: it belongs to the delivery ledger's lifecycle
// (migrations/000027), where a channel is asked for and finishes later. A direct
// mail is recorded once, at the moment the send is decided, so it has no first
// half to say `requested` about — and a caller that reached for the ledger's
// vocabulary is refused at the port rather than writing a row the table would
// take and nothing would ever finish.
const (
	MailSent       = "sent"
	MailSuppressed = "suppressed"
	MailFailed     = "failed"
)

// MaxMailReason bounds what a transport may say about refusing, in bytes. It is
// the record's own bound, not the transport's: the column allows more (000044
// caps at 500 so a hand-written row can be legible), and a record that lost the
// tail of a scrubbed sentence is still worth having, so a reason over the bound
// is clipped rather than refused.
const MaxMailReason = 200

// maxMailRecipient is the address bound of 000044. RFC 5321 allows 254 for one
// mailbox and 256 for a path; 320 is the column's roomier number, and a longer
// string than that is not an address.
const maxMailRecipient = 320

// maxMailRequestID is the request-id bound of 000044, which is what kit/httpx
// caps an id at (request_id.go's maxRequestID): a row cannot hold more of it
// than the router that minted it can hold.
const maxMailRequestID = 64

// MailRecord is one send, as the module that attempted it describes it. There is
// no subject, body, link or token field here, and adding one would be the leak:
// the mails this records are the ones whose secret is in the message and in no
// row, and the record exists to say the mail left without saying what was in it.
type MailRecord struct {
	// Kind is which mail this is, in the grammar every event name uses —
	// "auth.set_password", "auth.verification". The sender names its own kinds;
	// no shared module holds a list of them.
	Kind string
	// Recipient is the address the mail was named for, spelled by
	// contracts.EmailKey at the call site. Nothing here re-normalises it: that
	// would be the same fact normalised twice, in two ways that could disagree.
	Recipient string
	// Outcome is MailSent, MailSuppressed or MailFailed.
	Outcome string
	// Reason is empty with MailSent and required otherwise: a record that says
	// a mail did not go without saying why is a support ticket, not a record.
	// Carry it through RedactMailReason, which is what lets the transport's own
	// words into a row that must not hold a credential.
	Reason string
	// Traceparent and RequestID name the call that caused the send, and they are
	// the delivered event's own members (kit/events/transport/transport.go), not
	// the worker's: a handler is never handed a carried kit/trace context —
	// trace.With has exactly one writer, kit/httpx/request_id.go — so anything
	// read off the context is empty in every deployment that runs no collector.
	// Both are empty for a mail with no call behind it, and the row is NULL.
	Traceparent string
	RequestID   string
}

// MailLedger is the record of a mail that left the process with no notification
// row behind it.
//
// It is not the delivery ledger, and the two differ on purpose:
// notification_deliveries (000027) is one channel of one notice, keyed by
// notification_id, beginning at `requested` and ending at a terminal row, and
// delivery_ledger_coverage counts it. These mails have no notice, no channel and
// no `requested` half, and they are deliberately outside that register's number
// so the number keeps meaning what its name says. See modules/notification/README.md.
type MailLedger interface {
	// RecordMail appends one row, in tx — the transaction of the send it
	// describes. It never opens one of its own: a record that outlived the state
	// it records would be a ledger that can lie. A write that fails returns its
	// error, which rolls the attempt back: a transaction that cannot write its
	// own delivery record has not sent anything worth committing.
	RecordMail(ctx context.Context, tx db.Tx[db.Tenant], r MailRecord) error

	// MailOutcome is the newest outcome recorded for one request id, and
	// known=false when that request left no record — which is the ordinary
	// answer, the one every request that asked for no mail gets, and the reason
	// a caller reads "no record" as pending rather than as "the mail did not
	// go". An empty requestID is a caller's mistake and is refused as one: it
	// must not read the newest untraced row in the tenant.
	MailOutcome(ctx context.Context, tx db.Tx[db.Tenant], requestID string) (outcome string, known bool, err error)

	// NewestMailOutcome is the outcome of the newest record the caller's own
	// tenant holds, and known=false when it holds none. It answers the question a
	// request id cannot: what is the mail transport doing to the mails this
	// installation sends, including the one this caller asked for, whose record
	// may not exist because nobody has the address. That is why it exists, and
	// why it is the tenant's newest row rather than the caller's: see MailReport.
	NewestMailOutcome(ctx context.Context, tx db.Tx[db.Tenant]) (outcome string, known bool, err error)
}

// RedactMailReason turns what a transport said into what a record may say. It
// removes every occurrence of each secret it is handed — the token and sha256 of
// it, which is the pair modules/auth owns — collapses runs of whitespace to one
// space, trims, and clips to MaxMailReason bytes on a UTF-8 rune boundary.
//
// The bound it prices is the one modules/auth already refuses to pay twice: "a
// transport may quote its input in an error" (modules/auth/internal/
// verification.go). Recording "failed with the SMTP error" is worth an
// operator's time only if the sentence cannot carry the credential, and the
// case that proves the need is a mailer that echoes its input into its error.
//
// What it does not claim: a transport that *transforms* a secret in its error
// text — base64 of half of it, a hex dump — defeats a string scrub, which is
// why the reason is capped as well as scrubbed and why a record carries no
// subject, body, link or credential at all.
func RedactMailReason(said string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			said = strings.ReplaceAll(said, secret, "[redacted]")
		}
	}
	return clipMailReason(strings.Join(strings.Fields(said), " "))
}

// clipMailReason cuts to MaxMailReason bytes without ending mid-rune.
func clipMailReason(s string) string {
	if len(s) <= MaxMailReason {
		return s
	}
	cut := MaxMailReason
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// PrepareMail applies the table's own rules to a record and reports what 000044
// would refuse. Every implementation of MailLedger runs it before writing, so
// the fake and Postgres refuse the same record: an outcome outside the three, a
// record that is not `sent` with nothing to say about why, an address that is
// not one, and a kind that is not a name. A reason over MaxMailReason comes back
// clipped, not refused.
func PrepareMail(r MailRecord) (MailRecord, error) {
	r.Recipient = strings.TrimSpace(r.Recipient)
	r.Reason = clipMailReason(strings.TrimSpace(r.Reason))
	switch {
	case r.Recipient == "" || len(r.Recipient) > maxMailRecipient:
		return r, ErrMailRecipient
	case !transport.ValidName(r.Kind):
		return r, ErrMailKind
	case r.RequestID != "" && len(r.RequestID) > maxMailRequestID:
		return r, ErrMailRequest
	}
	switch r.Outcome {
	case MailSent:
		r.Reason = ""
	case MailSuppressed, MailFailed:
		if r.Reason == "" {
			return r, ErrMailReason
		}
	default:
		return r, ErrMailOutcome
	}
	return r, nil
}

// The five refusals PrepareMail makes, one error each so a caller can switch on
// the mistake rather than on the sentence: every one of them is a caller bug, and
// one that compared error text would be a caller that can be reworded out of its
// own check.
var (
	ErrMailRecipient = errors.New("notification: a mail record names the address it went to")
	ErrMailKind      = errors.New("notification: a mail record's kind is a name like auth.verification")
	ErrMailOutcome   = errors.New("notification: a mail record's outcome is sent, suppressed or failed")
	ErrMailReason    = errors.New("notification: a mail that did not go says why")
	ErrMailRequest   = errors.New("notification: a mail record's request id is the id one call was answered with")
)

// MailStatePending is what a caller answers when there is nothing to tell it,
// and it is not an outcome: no row ever says it. It lives here because both the
// door that answers it and the shell that reads it need the same one word.
const MailStatePending = "pending"

// MailReport is the one word a stranger may be told about one record: MailFailed
// when it says a transport refused that mail, MailStatePending for every other
// case, including a record that says the mail went.
//
// Why only the refusal is answerable. A public call that names an address mails
// only when somebody has an account there, and the routes that take the address
// answer neutrally on purpose (Reissue's enumeration defence, and the README's
// "acknowledgments are meant to be neutral and not reveal whether an account
// exists"). So any answer that differs between "a mail left" and "no mail left"
// is an answer about who has an account, one call after the acknowledgment took
// the trouble not to give one: `sent` for the addresses that are here, `pending`
// for the ones that are not. This function is where that is refused — not by
// hiding the record, which the operator reads, and not by answering `sent` about
// a mail that did not leave, which would be the lie the record exists to
// prevent.
//
// Which record is a second decision, and it belongs to the door rather than
// here: a caller whose own call left a record is answered from it, and one whose
// call left none is answered from the tenant's newest record
// (MailLedger.NewestMailOutcome), because "no record" is itself the answer to
// "does this address have an account?". A refusal is therefore sayable about the
// transport the caller's mail went through, which is one fact for everybody
// asking at that moment, and never about a mail the caller did not cause.
//
// What that still cannot be closed: a transport that refuses one address and
// takes another is a fact only about the address it refused, and any word about
// it identifies the address. That residue is the reason the shell's sentence on
// `failed` is about the mail server and not about the person's account, and it
// is why the door is bounded (contracts.MailDeliveryAsks) and same-site.
//
// What the caller cannot learn here is any of: that a mail went, that nobody has
// the address, which kind of mail it was, which address it named, or why it
// failed. Those stay in the row, which the tenant's own operator reads.
func MailReport(outcome string, known bool) string {
	if known && outcome == MailFailed {
		return MailFailed
	}
	return MailStatePending
}
