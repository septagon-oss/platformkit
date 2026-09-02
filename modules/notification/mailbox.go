package notification

import (
	"context"
	"log/slog"
	"slices"
	"sync"

	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// Mailbox is contracts.Mailer over a slice. It keeps every message and logs
// each one, so a development machine can read what would have been sent and a
// test can assert on it.
//
// It lives in the module and not in notificationtest, and the reason is a link
// rather than a taste. A deployment with no mail server wires this, so it is
// production code by use; notificationtest also holds a conformance suite,
// which imports "testing", and a package is linked whole — so the reference
// binary used to carry the testing package, its flag registrations and its
// init, because of where one struct sat. The test suite still uses this one:
// what a test wants and what an unconfigured deployment wants is the same
// thing, which is somewhere for a message to go that is not a mail server.
type Mailbox struct {
	mu   sync.Mutex
	sent []contracts.Message
}

// NewMailbox returns an empty mailbox.
func NewMailbox() *Mailbox { return &Mailbox{} }

var _ contracts.Mailer = (*Mailbox)(nil)

// Send records the message. It never fails: a mailbox that could refuse would
// be exercising the outbox's retry ladder rather than whatever is under test.
func (m *Mailbox) Send(ctx context.Context, msg contracts.Message) error {
	m.mu.Lock()
	m.sent = append(m.sent, msg)
	m.mu.Unlock()
	slog.InfoContext(ctx, "notification: mail is not configured, so this message was not sent",
		"to", msg.To, "subject", msg.Subject)
	return nil
}

// Sent is every message so far, in order.
func (m *Mailbox) Sent() []contracts.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.sent)
}
