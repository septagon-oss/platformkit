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
func SendMail(mailer contracts.Mailer, recipients contracts.RecipientLookup, hosts contracts.HostLookup, secure bool) events.Subscription {
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
				return nil
			}
			if err != nil {
				return err
			}
			to, err := address(ctx, tx, recipients, row.RecipientID)
			if err != nil || to == "" {
				return err
			}
			base, err := baseURL(ctx, tx, hosts, secure)
			if err != nil {
				return err
			}
			body, err := render(row, base)
			if err != nil {
				return err
			}
			return mailer.Send(ctx, contracts.Message{To: to, Subject: row.Title, Body: body})
		},
	}
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
