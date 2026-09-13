// Package mail describes delivery of an already rendered message. It owns no
// recipient directory, queue, notification persistence or rendering templates.
package mail

import "context"

// Message is one email, already rendered. Bodies may contain credentials and
// must not be logged or copied into generic notification/audit storage.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Mailer sends one message. Delivery is an external effect: a rollback cannot
// unsend mail, and an error may follow acceptance by the server. The caller owns
// retry/idempotency policy; each provider documents its cancellation behavior.
type Mailer interface {
	Send(ctx context.Context, m Message) error
}
