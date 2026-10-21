package internal_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

// A notice mail reaches the mailer with an HTML alternative beside its plain
// text: the same words, the link as an anchor on the tenant's own host, and
// every field the notice's author wrote escaped, so a title cannot become
// markup. A notice with no link still has both parts and no anchor.
func TestANoticeMailCarriesItsLinkAsAnAnchorInItsHTMLPart(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	svc := internal.NewService(directory{})
	box := notification.NewMailbox()
	sub := internal.SendMail(box, directory{}, hosts{}, nil, true)

	send := func(ctx context.Context, tx db.Tx[db.Tenant], notice contracts.Notice) contracts.Message {
		t.Helper()
		row, err := svc.Notify(ctx, tx, notice)
		if err != nil {
			t.Fatalf("notify: %v", err)
		}
		payload := `{"notificationId":"` + row.ID.String() + `","recipientId":"` + row.RecipientID.String() + `"}`
		before := len(box.Sent())
		if err := sub.Handler(ctx, tx, events.Event{
			ID: uuid.New(), Name: contracts.EventEmailRequested, Payload: []byte(payload),
		}); err != nil {
			t.Fatalf("send: %v", err)
		}
		sent := box.Sent()
		if len(sent) != before+1 {
			t.Fatalf("the mailbox holds %d messages, want %d", len(sent), before+1)
		}
		return sent[len(sent)-1]
	}

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		linked := send(ctx, tx, contracts.Notice{
			Recipient: notificationtest.Ada, Title: `Payout <b>"ready"</b> & settled`,
			Body: "Line one\nLine two", Link: "/app/payment/owner?day=2026-10-19&view=net", Wants: contracts.WantsEmail,
		})
		const anchor = `<a href="https://acme.example.com/app/payment/owner?day=2026-10-19&amp;view=net">`
		switch {
		case !strings.Contains(linked.Body, "https://acme.example.com/app/payment/owner?day=2026-10-19&view=net"):
			t.Errorf("the plain part lost its link: %q", linked.Body)
		case linked.HTML == "":
			t.Error("a notice mail with a link has no HTML part")
		case !strings.Contains(linked.HTML, anchor):
			t.Errorf("the HTML part does not carry the link as an anchor on the tenant's host, want %s in %q", anchor, linked.HTML)
		case !strings.Contains(linked.HTML, "Line one\nLine two"):
			t.Errorf("the HTML part does not carry the body the plain part carries: %q", linked.HTML)
		case strings.Contains(linked.HTML, "<b>"):
			t.Errorf("the title reached the HTML part as markup: %q", linked.HTML)
		case !strings.Contains(linked.HTML, "Payout &lt;b&gt;&#34;ready&#34;&lt;/b&gt; &amp; settled"):
			t.Errorf("the HTML part does not carry the title escaped: %q", linked.HTML)
		}

		bare := send(ctx, tx, contracts.Notice{
			Recipient: notificationtest.Ada, Title: "Your export is ready", Wants: contracts.WantsEmail,
		})
		if bare.Body == "" || !strings.Contains(bare.HTML, "Your export is ready") || strings.Contains(bare.HTML, "<a ") {
			t.Errorf("a notice with no link = text %q and HTML %q, want both parts and no anchor", bare.Body, bare.HTML)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("the transaction: %v", err)
	}
}
