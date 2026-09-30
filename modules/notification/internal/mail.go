package internal

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"text/template"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// templates are the module's own, embedded, and there is one: a generic notice.
// A per-event template is a screen's worth of decisions and belongs to whoever
// raises the notice, so it arrives as a title and a body rather than as a name
// this package would have to know.
//
// text/template and not html/template: this is a plain-text message, and HTML
// escaping in it would turn an apostrophe into &#39; in somebody's inbox.
//
//go:embed templates/*.tmpl
var templates embed.FS

var notice = template.Must(template.ParseFS(templates, "templates/*.tmpl"))

// SendMail is the subscription that sends the message.
//
// It is a subscriber and not part of Notify for the reason the event exists:
// the request that raised the notice commits its row and returns, and the
// worker talks to the mail server. So a slow relay costs nobody a response, a
// failure is retried by the outbox on the kernel's own ladder, and a message
// that can never be sent ends in platformkit_dead_letters rather than in a log
// line somebody has to notice.
//
// What it is handed is two identifiers. The row, the address and the host are
// all read here, inside the transaction the kernel opened in the event's own
// tenant — see contracts.EmailRequested for why the payload is that thin, and
// for the two consequences: a notice deleted in the meantime is a skip, and an
// address changed in the meantime is the one the mail goes to.
func SendMail(mailer contracts.Mailer, recipients contracts.RecipientLookup, hosts contracts.HostLookup, senders contracts.Senders, secure bool) events.Subscription {
	return events.Subscription{
		Module: "notification", Name: contracts.EventEmailRequested,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			var req contracts.EmailRequested
			if err := json.Unmarshal(ev.Payload, &req); err != nil {
				return fmt.Errorf("notification: read the mail request: %w", err)
			}
			row, err := crud.Get[*contracts.Notification](tx, req.NotificationID)
			if errors.Is(err, crud.ErrNotFound) {
				// Somebody deleted the notice between the request and the send.
				// That is an answer rather than a failure — there is nothing to
				// say any more — and it is logged because a mail that silently
				// never arrives is the hardest kind of bug to be told about.
				slog.InfoContext(ctx, "notification: the notice was gone before its mail was sent",
					"notification", req.NotificationID, "recipient", req.Recipient)
				return record(tx, req.NotificationID, ChannelEmail, OutcomeSuppressed, "the notice was deleted before its mail was sent")
			}
			if err != nil {
				return err
			}
			// A delivery the ledger already closed is not sent again. The outbox
			// redelivers anything it did not see acknowledged — the answer was
			// lost, the worker died after the send — and a second attempt at a
			// message the person already received is the thing this table exists to
			// make visible. Seeing it and *not sending* is the other half of the
			// partial unique index 000030: the index stops the second row, this
			// stops the second message.
			done, err := closed(tx, row.ID, ChannelEmail)
			if err != nil {
				return err
			}
			if done {
				slog.InfoContext(ctx, "notification: the mail was already delivered; the redelivery was acknowledged",
					"notification", row.ID, "recipient", req.Recipient)
				return nil
			}
			to, err := address(ctx, tx, recipients, row.RecipientID)
			if err != nil {
				return err
			}
			if to == "" {
				return record(tx, row.ID, ChannelEmail, OutcomeSuppressed, "the recipient had no email address when the mail was due")
			}
			base, err := baseURL(ctx, tx, hosts, secure)
			if err != nil {
				return err
			}
			body, err := render(row, base)
			if err != nil {
				return err
			}
			// The tenant's own sender, read in this transaction — the same one the
			// notice's decision read, so the mail leaves as the tenant the person's
			// settings were applied to. Nil is the deployment's own configured
			// address, which is what an installation that never wired per-tenant
			// identity has always sent from, and the header says so plainly rather
			// than borrowing a name nobody claimed.
			var sender *contracts.Sender
			if senders != nil {
				if sender, err = senders.For(ctx, tx); err != nil {
					return err
				}
				if sender == nil {
					return record(tx, row.ID, ChannelEmail, OutcomeSuppressed,
						"this tenant has no sender: mail is refused until its administrator sets one")
				}
			}
			msg := contracts.Message{To: to, Subject: row.Title, Body: body, Lang: "en"}
			if sender != nil {
				msg.Sender, msg.ReplyTo = sender, sender.ReplyTo
			}
			if err := mailer.Send(ctx, msg); err != nil {
				// Two answers, and the ledger distinguishes them. A transient one —
				// the connection dropped, the relay is down — writes no row: this
				// transaction rolls back and the outbox retries on the kernel's
				// ladder, which is the only record a retry needs.
				//
				// A permanent one (contracts.ErrPermanent: the relay's 5xx, a
				// signature this deployment cannot make) writes failed with what the
				// relay said and returns nil, so the message is acknowledged. A
				// permanent failure that is retried is a channel with no terminal row
				// until the dead letter, and dead letters are not a ledger.
				if errors.Is(err, contracts.ErrPermanent) {
					return record(tx, row.ID, ChannelEmail, OutcomeFailed, reason(err))
				}
				return err
			}
			return record(tx, row.ID, ChannelEmail, OutcomeSent, "")
		},
	}
}

// closed reports whether this channel already has a terminal row, which is the
// question a redelivered event asks before it touches anybody else's machine.
func closed(tx db.Tx[db.Tenant], notification uuid.UUID, channel string) (bool, error) {
	var n int64
	err := tx.DB().Raw(`
		SELECT count(*) FROM notification_deliveries
		WHERE notification_id = ? AND channel = ? AND outcome IN ('sent', 'suppressed', 'failed')`,
		notification, channel).Row().Scan(&n)
	return n > 0, err
}

// maxReason bounds what a relay's answer can put in the ledger: the sentence is
// for an operator, and a 2 kB SMTP greeting is a fact about a server, not about
// a delivery. The words that matter — the code and the first line — come first.
const maxReason = 500

func reason(err error) string {
	msg := strings.TrimPrefix(err.Error(), "notification: ")
	if len(msg) > maxReason {
		return msg[:maxReason]
	}
	return msg
}

// baseURL is the scheme and the host this tenant's people reach the application
// at, which is what a path in a notice has to become for a mail client.
//
// The host is the tenant's own — a link built from the application's public
// host would send one customer's people to another customer's front door — and
// the scheme is the deployment's, decided once where the session cookie's
// Secure flag is decided, so http://localhost still works on a laptop.
func baseURL(ctx context.Context, tx db.Tx[db.Tenant], hosts contracts.HostLookup, secure bool) (string, error) {
	if hosts == nil {
		return "", nil
	}
	host, err := hosts.PublicHost(ctx, tx)
	if err != nil {
		return "", fmt.Errorf("notification: find the host of %s: %w", db.TenantOf(tx).Slug, err)
	}
	if host == "" {
		return "", nil
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	return scheme + "://" + host, nil
}

// render is the notice template, filled in from the row.
func render(row *contracts.Notification, base string) (string, error) {
	var out strings.Builder
	err := notice.ExecuteTemplate(&out, "notice.tmpl", struct {
		*contracts.Notification
		Host    string
		BaseURL string
	}{row, strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://"), base})
	if err != nil {
		return "", fmt.Errorf("notification: render the notice: %w", err)
	}
	return out.String(), nil
}
