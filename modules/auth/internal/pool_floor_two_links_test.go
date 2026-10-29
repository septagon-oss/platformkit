package internal_test

// One credential hand-off now brackets the mail server inside a database
// transaction: that is what carries a person's mail slot past the commit that
// released every other lock. A transaction keeps one connection of the application
// pool for its whole life, and the marks of the transport's answer are written in a
// detached transaction of their own — so a link in the air holds two pooled
// connections at once, one of them parked on a mail server for as long as that
// server takes.
//
// Two connections is the smallest pool the kernel accepts: kit/app refuses fewer,
// because a detached HTTP write and a job's advisory lock each need a second one, and
// config.example.yaml states the same floor. At that floor, two people mailed in the
// same moment cannot both have their send recorded: each hand-off is waiting for a
// connection that only the other's hand-off can free — by giving up. By then the mail
// has left, so what a retry finds is a person holding a link the table never marked
// sent, and the next ask mints a second link over the one already in the mailbox.

import (
	"context"
	"database/sql"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// twoInAirTransport is the mail host that accepts both connections and answers
// neither until both links have arrived: the ordinary shape of a server that is
// there and slow rather than gone.
type twoInAirTransport struct {
	mu      sync.Mutex
	inside  int
	both    chan struct{}
	once    sync.Once
	release chan struct{}
	box     authtest.Mailbox
}

func (m *twoInAirTransport) Send(ctx context.Context, message notification.Message) error {
	m.mu.Lock()
	m.inside++
	second := m.inside == 2
	m.mu.Unlock()
	if second {
		m.once.Do(func() { close(m.both) })
	}
	select {
	case <-m.release:
	case <-time.After(60 * time.Second):
		return context.DeadlineExceeded
	}
	return m.box.Send(ctx, message)
}

// TestTwoMailedLinksAtTheKernelsPoolFloorAreEachRecordedAsSent asks two people for a
// reset link at one moment on an installation sized at the smallest pool the kernel
// accepts, and asks what remains known about both mails once the host lets them go.
func TestTwoMailedLinksAtTheKernelsPoolFloorAreEachRecordedAsSent(t *testing.T) {
	adminURL, appURL := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, migrations.Source, usermodule.Migrations,
		notificationmodule.Migrations, auth.Migrations, audit.Migrations); err != nil {
		t.Fatalf("migrate the installation: %v", err)
	}
	admin := dbtest.Open(t, adminURL)
	conn, err := db.OpenWithPool(t.Context(), appURL, db.Pool{MaxOpenConns: 2, MaxIdleConns: 2})
	if err != nil {
		t.Fatalf("open the application pool at the kernel's floor: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	transport := &twoInAirTransport{both: make(chan struct{}), release: make(chan struct{})}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.Mailer, d.Mails = transport, mailLedger()
	})
	person(t, conn, "ada@acme.localhost", contracts.RoleMember)
	person(t, conn, "bob@acme.localhost", contracts.RoleMember)

	// Both asks commit before either link is carried, so neither person's request is
	// the one still holding a transaction when the two hand-offs meet.
	for _, address := range []string{"ada@acme.localhost", "bob@acme.localhost"} {
		if res := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot",
			`{"email":"`+address+`"}`); res.Code != 200 {
			t.Fatalf("forgot(%s)=%d %s, want 200", address, res.Code, res.Body.String())
		}
	}
	var payloads [][]byte
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, err := tx.DB().Raw("SELECT payload FROM platformkit_outbox WHERE name = ? ORDER BY created_at, id",
			contracts.EventResetRequested).Rows()
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var payload []byte
			if err := rows.Scan(&payload); err != nil {
				return err
			}
			payloads = append(payloads, payload)
		}
		return rows.Err()
	})
	if err != nil || len(payloads) != 2 {
		t.Fatalf("reset requests in the outbox=%d err=%v, want the two asked for", len(payloads), err)
	}
	var reset events.Subscription
	for _, s := range subs {
		if s.Name == contracts.EventResetRequested {
			reset = s
		}
	}
	if reset.Name == "" {
		t.Fatal("no reset subscription to deliver to")
	}

	// Two reset requests, two people, one moment: each commits its credential and
	// hands its link over, and each hand-off holds the pool while the host has it.
	var wg sync.WaitGroup
	errs := make([]error, len(payloads))
	for i, payload := range payloads {
		wg.Add(1)
		go func(i int, payload []byte) {
			defer wg.Done()
			errs[i] = db.Run(deliveryContext(context.WithoutCancel(t.Context()), conn, acme), conn,
				func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					return reset.Handler(ctx, tx, events.Event{
						ID: uuid.New(), Name: reset.Name, TenantID: acme.ID, Payload: payload,
					})
				})
		}(i, payload)
	}

	select {
	case <-transport.both:
	case <-time.After(90 * time.Second):
		close(transport.release)
		wg.Wait()
		transport.mu.Lock()
		inside := transport.inside
		transport.mu.Unlock()
		stats := conn.Stats()
		t.Fatalf("the host was never handed both links at once, so nothing below was reached: sends entered=%d"+
			" links mailed=%d pool connections in use=%d pool waits=%d marked sent=%d errs=%v",
			inside, len(transport.box.Sent()), stats.InUse, int(stats.WaitCount),
			linksMarkedSent(t, admin), errs)
	}
	close(transport.release)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("deliver reset request %d: %v", i+1, err)
		}
	}
	if sent := transport.box.Sent(); len(sent) != 2 {
		t.Fatalf("links mailed=%d, want the two each person asked for", len(sent))
	}
	marked := linksMarkedSent(t, admin)
	_, _, records := outcomeOf(t, conn, contracts.MailSetPassword)
	if marked != 2 || records != 2 {
		t.Errorf("links marked sent=%d and ledger records=%d, want 2 and 2: a mail that left must be"+
			" recorded, or the next ask mints a second link over the one in the mailbox", marked, records)
	}
}

// linksMarkedSent counts the credentials the table says left: what a hand-off left
// behind is a fact about the table, not about one tenant's view of it.
func linksMarkedSent(t *testing.T, admin *sql.DB) int {
	t.Helper()
	var n int
	if err := admin.QueryRow("SELECT count(*) FROM password_tokens WHERE sent_at IS NOT NULL").Scan(&n); err != nil {
		t.Fatalf("count mailed links: %v", err)
	}
	return n
}
