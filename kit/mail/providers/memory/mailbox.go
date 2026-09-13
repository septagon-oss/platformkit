// Package memory retains outgoing messages in memory without sending them.
package memory

import (
	"context"
	"log/slog"
	"slices"
	"sync"

	"github.com/septagon-oss/platformkit/kit/mail"
)

// Mailbox records messages in memory and logs only recipient and subject,
// never body. It is a provider for tests and explicitly unconfigured mail
// delivery; importing it brings no test harness into an application.
type Mailbox struct {
	mu   sync.Mutex
	sent []mail.Message
}

// New returns an empty mailbox.
func New() *Mailbox { return &Mailbox{} }

var _ mail.Mailer = (*Mailbox)(nil)

// Send records the message. It never fails: a mailbox that could refuse would
// be exercising the outbox's retry ladder rather than whatever is under test.
func (m *Mailbox) Send(ctx context.Context, msg mail.Message) error {
	m.mu.Lock()
	m.sent = append(m.sent, msg)
	m.mu.Unlock()
	slog.InfoContext(ctx, "notification: mail is not configured, so this message was not sent",
		"to", msg.To, "subject", msg.Subject)
	return nil
}

// Sent is every message so far, in order.
func (m *Mailbox) Sent() []mail.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.sent)
}
