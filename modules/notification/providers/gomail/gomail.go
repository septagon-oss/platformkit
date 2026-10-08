// Package gomail is the notification module's email carrier: SMTP through
// github.com/wneessen/go-mail, the standard the register names for this pillar.
//
// It is a package of its own, outside the module's internal/, for the same
// reason kit/events/providers/{memory,nats} are: the SDK belongs to the carrier,
// not to the module. Nothing in modules/notification imports this package — the
// composition does, and hands it over as a contracts.Mailer — so a deployment
// that sends no mail links no SMTP client, and the module's own tests stay about
// decisions rather than about a third party's wire format.
//
// It replaces the stdlib net/smtp sender this module shipped with. The reason is
// the brief's ("go-mail, the standard the register names") and it buys three
// things a hand-rolled conversation did not have: a bounded dial, write and read
// on one context, multipart/alternative for a message that has a text part and
// an HTML one, and DKIM signing (RFC 6376) — which is what makes a tenant's own
// sender more than a string in a header that any relay could have added.
package gomail

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	mail "github.com/wneessen/go-mail"

	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// Config is one outgoing mail server. It has the shape of config.Mail so the
// composition converts one in a line, and it is a struct of this package rather
// than an alias of kit/config so the carrier does not read a deployment's file.
//
// EnvelopeFrom is the MAIL FROM address: one bare address, still one per binary,
// and still the value kit/config refuses with a display name attached. It is not
// the sender a person sees — that is Message.Sender, the tenant's own, and the
// two answer different questions (see contracts.Sender).
type Config struct {
	Host         string
	Port         int
	Username     string
	Password     string
	EnvelopeFrom string
	// TLS asks whether the connection must be encrypted. The default, false, is
	// opportunistic: STARTTLS when the server offers it and plaintext when a
	// private relay offers nothing, which is what the stdlib sender did and what
	// a deployment on its own network expects. True is TLSMandatory — a
	// deployment that mails over the public internet sets it, because a
	// silent fall back to plaintext is a credential sent in the clear.
	TLS bool
	// Timeout bounds the dial and every read and write after it. Zero takes the
	// library's default. A send that never returns is a send the outbox can
	// neither retry nor dead-letter, which is the bug the stdlib sender had.
	Timeout time.Duration
}

// Sender is the production contracts.Mailer: one client per message, dialed,
// used and closed, because this runs in the worker one event at a time and a
// pool would buy nothing.
type Sender struct{ cfg Config }

// New returns the carrier for cfg. The composition wires this, or
// notification.NewMailbox() when no host is configured, and says which it chose
// at boot.
func New(cfg Config) *Sender { return &Sender{cfg: cfg} }

var _ contracts.Mailer = (*Sender)(nil)

// Send renders one contracts.Message onto the wire and hands it to the relay.
//
// The address in the From *header* is the tenant's (Message.Sender), the address
// on the envelope is the deployment's (Config.EnvelopeFrom), and the signature is
// the tenant's: DKIM signs the domain in the header, so the key that goes on the
// message is the one this deployment holds for that domain and selector, from
// contracts.Sender.Key — which the composition read out of its own key store and
// no table ever held.
//
// A relay's 5xx comes back wrapped in contracts.ErrPermanent, which is how the
// worker knows a second attempt would say the same thing: it writes the failed
// row and acknowledges. Everything else — a dropped connection, a 4xx, a timeout
// — is returned as it arrived, the transaction rolls back with no ledger row, and
// the outbox retries on the kernel's ladder.
func (s *Sender) Send(ctx context.Context, m contracts.Message) error {
	if m.To == "" {
		return contracts.Permanent("the message has no recipient")
	}
	// Text first, always: go-mail refuses to send a message with no body, and a
	// mail client that is handed HTML with no text alternative shows the source.
	if m.Body == "" {
		return contracts.Permanent("the message has no text body")
	}
	msg, err := s.message(m)
	if err != nil {
		return err
	}
	opts := []mail.Option{mail.WithPort(s.port())}
	if s.cfg.Username != "" {
		// Username and password alone are not authentication: go-mail defaults to
		// SMTPAuthNoAuth, so a client built with credentials it was never told to
		// speak sends them nowhere and takes the relay's 530 — which it classifies
		// as permanent, so every mail this deployment mails would be recorded failed
		// and acknowledged. The mechanism is PLAIN (RFC 4616), the one the stdlib
		// sender this package replaced used through smtp.PlainAuth: net/smtp refuses
		// to offer it over plaintext to a relay that is not the local machine, which
		// is the same judgement Config.TLS asks the operator to make explicit.
		opts = append(opts, mail.WithUsername(s.cfg.Username), mail.WithPassword(s.cfg.Password),
			mail.WithSMTPAuth(mail.SMTPAuthPlain))
	}
	if s.cfg.TLS {
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	} else {
		opts = append(opts, mail.WithTLSPolicy(mail.TLSOpportunistic))
	}
	if s.cfg.Timeout != 0 {
		opts = append(opts, mail.WithTimeout(s.cfg.Timeout))
	}
	client, err := mail.NewClient(s.cfg.Host, opts...)
	if err != nil {
		return fmt.Errorf("notification: prepare the mail client for %s: %w", s.cfg.Host, err)
	}
	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		return s.failed(fmt.Sprintf("relay %s", s.cfg.Host), err)
	}
	return nil
}

// message is the parts of the library this carrier touches in one place: the
// two addresses, the two bodies, the signature. It takes no context because it
// talks to nobody — everything it needs is in the message it was handed, which is
// what makes a test of the wire format possible without a relay.
func (s *Sender) message(m contracts.Message) (*mail.Msg, error) {
	msg := mail.NewMsg()
	if err := msg.To(m.To); err != nil {
		return nil, contracts.Permanent(fmt.Sprintf("%s is not a mail address: %s", m.To, err))
	}
	if err := msg.EnvelopeFrom(s.cfg.EnvelopeFrom); err != nil {
		return nil, fmt.Errorf("notification: %s is not a usable envelope sender: %w", s.cfg.EnvelopeFrom, err)
	}
	if m.Sender != nil {
		// The gate the worker already applied, applied again at the door: an identity
		// that stopped being believed after the message was queued must not leave.
		if m.Sender.Status != contracts.SenderVerified {
			return nil, contracts.Permanent("this tenant's sender " + m.Sender.Header() +
				" is " + m.Sender.Status + ", not verified, so nothing sends as it")
		}
		// Header() is RFC 5322's quoted form, so a display name with an apostrophe or
		// a comma arrives as a name rather than as a broken header — the reason this
		// goes through net/mail in contracts rather than through fmt.Sprintf here.
		if err := msg.From(m.Sender.Header()); err != nil {
			return nil, contracts.Permanent(fmt.Sprintf("%s cannot be a From header: %s", m.Sender.Header(), err))
		}
		if err := sign(msg, m.Sender); err != nil {
			return nil, err
		}
	} else if err := msg.From(s.cfg.EnvelopeFrom); err != nil {
		return nil, fmt.Errorf("notification: %s is not a usable From header: %w", s.cfg.EnvelopeFrom, err)
	}
	if m.ReplyTo != "" {
		if err := msg.ReplyTo(m.ReplyTo); err != nil {
			return nil, contracts.Permanent(fmt.Sprintf("%s is not a reply-to address: %s", m.ReplyTo, err))
		}
	}
	msg.Subject(strings.Join(strings.Fields(m.Subject), " "))
	msg.SetBodyString(mail.TypeTextPlain, m.Body)
	if m.HTML != "" {
		msg.AddAlternativeString(mail.TypeTextHTML, m.HTML)
	}
	return msg, nil
}

// sign puts the DKIM signature (RFC 6376) on the message: the domain and the
// selector come from the tenant's row, the key from the deployment.
//
// A sender with no key is a refusal rather than an unsigned message. Sending
// unsigned mail from a domain that publishes a DKIM record is how a domain's
// reputation is spent by the deployment that did not notice the record; the
// channel gets a failed row naming the deployment instead, which is the sentence
// an operator can act on. contracts.Decide already suppresses this case before a
// notice is asked of a worker — this is the door a mail that did not come from a
// notice (a module's own mail, a retry after a key was withdrawn) walks through.
func sign(msg *mail.Msg, s *contracts.Sender) error {
	if len(s.Key) == 0 {
		return contracts.Permanent("this deployment holds no DKIM key to sign as " + s.DKIMName())
	}
	key, err := mail.PrivKeyFromPEM(s.Key)
	if err != nil {
		return contracts.Permanent(fmt.Sprintf("the DKIM key for %s cannot be read: %s", s.DKIMName(), err))
	}
	msg.SetDKIM(mail.NewDKIMSigner(s.Domain, s.Selector, key))
	return nil
}

// failed turns what the relay said into the two kinds of answer the ledger
// distinguishes. A 5xx is a refusal that will not change; go-mail's own IsTemp is
// consulted first because it knows which of its own failures are transient ones
// (a connection that dropped mid-send looks like nothing) and which are the
// server's verdict.
func (s *Sender) failed(what string, err error) error {
	var send *mail.SendError
	if !errors.Is(err, context.DeadlineExceeded) && errors.As(err, &send) && !send.IsTemp() &&
		send.ErrorCode() >= 500 && send.ErrorCode() < 600 {
		return contracts.Permanent(fmt.Sprintf("%s refused the message: %s", what, send))
	}
	return fmt.Errorf("notification: send by %s: %w", what, err)
}

func (s *Sender) port() int {
	if s.cfg.Port <= 0 || s.cfg.Port > 65535 {
		return mail.DefaultPort
	}
	return s.cfg.Port
}

// String names the relay in a log line without its password.
func (s *Sender) String() string { return s.cfg.Host + ":" + strconv.Itoa(s.port()) }
