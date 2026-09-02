package internal

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

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
// host is the application's public host: it is the "from" of the footer and the
// prefix of the link, because a notice's link is a path and a mail client needs
// a URL.
func SendMail(mailer contracts.Mailer, host string) events.Subscription {
	return events.Subscription{
		Module: "notification", Name: contracts.EventEmailRequested,
		Handler: func(ctx context.Context, _ db.Tx[db.Tenant], ev events.Event) error {
			var req contracts.EmailRequested
			if err := json.Unmarshal(ev.Payload, &req); err != nil {
				return fmt.Errorf("notification: read the mail request: %w", err)
			}
			if req.To == "" {
				return nil
			}
			body, err := render(req, host)
			if err != nil {
				return err
			}
			return mailer.Send(ctx, contracts.Message{To: req.To, Subject: req.Title, Body: body})
		},
	}
}

// render is the notice template, filled in. It takes the request rather than
// the notification row because the payload carries the whole message: see
// contracts.EmailRequested.
func render(req contracts.EmailRequested, host string) (string, error) {
	var out strings.Builder
	err := notice.ExecuteTemplate(&out, "notice.tmpl", struct {
		contracts.EmailRequested
		Host    string
		BaseURL string
	}{req, host, "https://" + host})
	if err != nil {
		return "", fmt.Errorf("notification: render the notice: %w", err)
	}
	return out.String(), nil
}
