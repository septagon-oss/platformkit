// Package notificationtest is the conformance suite for contracts.Service, a
// fake that passes it, and the in-memory Mailer.
//
// The Mailbox is here rather than in the module because it is the same thing a
// test wants and a deployment with no mail server wants: somewhere for a
// message to go that is not a mail server. main wires it when config.Mail names
// no host, which is why this package holds no test-only import in the file that
// declares it.
package notificationtest

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
