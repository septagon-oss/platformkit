package internal_test

// Two forgotten-password requests for one person, delivered together, are the case
// the per-person lock exists for: whichever one reads "no link yet" would mail a
// second link of which the table keeps only the last. The lock is an advisory lock
// on a transaction, and the send leaves on that transaction's commit, so a request
// that arrives while the first message is still in the air meets no lock at all.
// This case holds the transport open to reach that moment and asks what leaves.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// heldTransport is a contracts.Mailer that holds the first message it is handed
// until the test lets it go, and takes every later one at once. It records what
// left, so the case reads the transport rather than a log line.
type heldTransport struct {
	mu      sync.Mutex
	box     authtest.Mailbox
	calls   int
	entered chan struct{}
	release chan struct{}
}

func (m *heldTransport) Send(ctx context.Context, message notification.Message) error {
	m.mu.Lock()
	m.calls++
	first := m.calls == 1
	m.mu.Unlock()
	if first {
		close(m.entered)
		<-m.release
	}
	return m.box.Send(ctx, message)
}

// TestASecondResetAskedWhileTheFirstLinkIsInTheAirMailsOneLink asks what one person
// is mailed when their second request arrives while the transport still holds their
// first link. One link leaves, one row holds it, and the second request reads the
// first one's link rather than mailing over it.
func TestASecondResetAskedWhileTheFirstLinkIsInTheAirMailsOneLink(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	transport := &heldTransport{entered: make(chan struct{}), release: make(chan struct{})}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.Mailer, d.Mails = transport, mailLedger()
	})
	person(t, conn, "ada@acme.localhost", contracts.RoleMember)
	first, second := askedFor(t, conn, router, 0), askedFor(t, conn, router, 1)
	handle := resetSubscription(t)

	// The first request, committed, its mail held by the transport.
	delivered := make(chan error, 1)
	go func() { delivered <- deliverOne(t, conn, handle, first) }()

	select {
	case <-transport.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("the transport was never handed the first link, so nothing below was reached")
	}

	// The same person asks again while that message is still in the air.
	if err := deliverOne(t, conn, handle, second); err != nil {
		t.Errorf("deliver the second reset request: %v", err)
	}
	close(transport.release)
	if err := <-delivered; err != nil {
		t.Errorf("deliver the first reset request: %v", err)
	}

	if sent := len(transport.box.Sent()); sent != 1 {
		t.Errorf("links mailed=%d, want one: the second request found a link being sent"+
			" and mailed a second one the table cannot keep", sent)
	}
	mailedLinkNamesTheRowTheTableHolds(t, admin, transport.box.Sent()[0].Body)
	var rows int
	if err := admin.QueryRow("SELECT count(*) FROM password_tokens").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("password tokens=%d, want the one row the single pending write leaves", rows)
	}
}
