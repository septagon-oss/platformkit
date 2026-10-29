package internal

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"log/slog"
	"strings"
	"text/template"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// templates are the module's own, embedded, and there is one notice in two
// parts. A per-event template is a screen's worth of decisions and belongs to
// whoever raises the notice, so it arrives as a title and a body rather than as
// a name this package would have to know.
//
// The plain-text part is text/template, because HTML escaping in it would turn
// an apostrophe into &#39; in somebody's inbox. Its HTML alternative is
// html/template, which escapes every field the notice's author wrote, so no
// module raising a notice writes or injects markup.
//
//go:embed templates/*.tmpl
var templates embed.FS

var (
	notice     = template.Must(template.ParseFS(templates, "templates/notice.tmpl"))
	noticeHTML = htmltemplate.Must(htmltemplate.ParseFS(templates, "templates/notice.html.tmpl"))
)

// SendMail is the subscription that turns a mail request into a duty: it decides
// the mail — the row, the address, the host, the rendered parts — and writes the
// intent, in the transaction the kernel opened for the event. It never opens a
// socket.
//
// The sending moved. A relay dialled inside this transaction was a mail a rollback
// could undo after it had left, and a relay whose answer was never written left no
// row at all, so the outbox redelivered the event, the person was mailed twice and
// nothing said so. The intent row closes both halves. The row, the address and the
// host are all read here, in the event's own tenant — see contracts.EmailRequested
// for why the payload is that thin — and the worker asks both again before the
// socket opens, because a queue is where the gap between a decision and its send
// lives.
func SendMail(svc *Service, hosts contracts.HostLookup, secure bool) events.Subscription {
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
			// partial unique index 000056: the index stops the second row, this
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
			to, err := address(ctx, tx, svc.recipients, row.RecipientID)
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
			html, err := renderHTML(row, base)
			if err != nil {
				return err
			}
			if svc.mailer == nil || contracts.IsNoTransport(svc.mailer) {
				// No transport in this installation, said on the delivery ledger
				// rather than in a log line somebody has to notice: the notice stays
				// and stays readable in the application, and the mail it asked for is
				// recorded as suppressed with the reason a reader can act on. The
				// outbox does not retry what no retry can fix.
				return record(tx, row.ID, ChannelEmail, OutcomeSuppressed,
					"this installation has no mail server: set mail.host")
			}
			// The tenant's own sender, read in this transaction — the same one the
			// notice's decision read, so the mail leaves as the tenant the person's
			// settings were applied to. Nil is the deployment's own configured
			// address, which is what an installation that never wired per-tenant
			// identity has always sent from, and the header says so plainly rather
			// than borrowing a name nobody claimed.
			var sender *contracts.Sender
			if svc.senders != nil {
				if sender, err = svc.senders.For(ctx, tx); err != nil {
					return err
				}
				if sender == nil {
					return record(tx, row.ID, ChannelEmail, OutcomeSuppressed,
						"this tenant has no sender: mail is refused until its administrator sets one")
				}
				// Verification is re-read here, and not carried in from the moment the
				// notice was raised. The request decided against the sender that existed
				// then; between that commit and this one the administrator can save a
				// different domain or selector, which takes the row back to pending, and
				// a queue is exactly where that gap lives. Signing as a domain that stopped
				// vouching for itself is the refusal this module exists to make, so the
				// check is against the row this transaction reads and the answer is terminal:
				// a failed or suppressed row, not a retry that says the same thing.
				if sender.Status != contracts.SenderVerified {
					return record(tx, row.ID, ChannelEmail, OutcomeSuppressed,
						"this tenant's sender is "+sender.Status+", not verified: the mail was queued while it was, and nothing sends as an identity that stopped being believed")
				}
			}
			// The mail is owed, so it is written rather than sent. The cause is this
			// event's own id, which is the identity the outbox keeps across a
			// redelivery and an operator's replay of the same event: one mail request
			// is one intent, and the unique partial index of 000076 is what makes that
			// a fact rather than an intention. ReplyTo is left empty on purpose — the
			// worker sets it from the tenant's sender when it sends, and a reply
			// address copied here would be a stale reading of a row the tenant may
			// still change.
			_, _, err = svc.Queue(ctx, tx, contracts.OutgoingMail{
				Purpose: contracts.EventEmailRequested,
				Cause:   ev.ID.String(),
				Person:  row.RecipientID,
				Notice:  row.ID,
				Message: contracts.Message{To: to, Subject: row.Title, Body: body, HTML: html, Lang: "en"},
			})
			return err
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
	return clipReason(strings.TrimPrefix(err.Error(), "notification: "))
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

// render is the notice's plain-text part, filled in from the row.
func render(row *contracts.Notification, base string) (string, error) {
	var out strings.Builder
	if err := notice.ExecuteTemplate(&out, "notice.tmpl", fields(row, base)); err != nil {
		return "", fmt.Errorf("notification: render the notice: %w", err)
	}
	return out.String(), nil
}

// renderHTML is the same notice as the HTML alternative: the same words, and the
// link as an anchor on the tenant's host. With no host to build on the link is
// written as the bare path, as the plain part writes it, and is not an anchor: a
// mail client has nothing to resolve a path against.
func renderHTML(row *contracts.Notification, base string) (string, error) {
	var out strings.Builder
	if err := noticeHTML.ExecuteTemplate(&out, "notice.html.tmpl", fields(row, base)); err != nil {
		return "", fmt.Errorf("notification: render the notice's HTML: %w", err)
	}
	return out.String(), nil
}

// fields is what both parts of a notice are filled in from: the row, the host a
// person knows the application by, and the base its link is built on.
func fields(row *contracts.Notification, base string) any {
	return struct {
		*contracts.Notification
		Host    string
		BaseURL string
	}{row, strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://"), base}
}
