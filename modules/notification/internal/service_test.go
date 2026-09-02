package internal_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

var (
	acme        = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	globex      = tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	errRollback = errors.New("rolled back on purpose")
)

// directory is the RecipientLookup the suite needs: Ada has an address and Bob
// does not, which is what makes "a recipient with no address gets the row and
// no mail" a case rather than a claim.
type directory struct{}

func (directory) Email(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (string, error) {
	if id == notificationtest.Ada {
		return notificationtest.AdaEmail, nil
	}
	return "", crud.ErrNotFound
}

// TestServiceConforms runs the same suite the fake runs, against the real
// service, a real Postgres and a real tenant transaction.
func TestServiceConforms(t *testing.T) {
	notificationtest.RunService(t, func(t *testing.T, run func(notificationtest.Fixture)) {
		_, conn := dbtest.Schema(t)
		svc := internal.NewService(directory{})
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(notificationtest.Fixture{
				Ctx: ctx, Tx: tx, Service: svc,
				Published: func() []string { return outbox(t, tx) },
			})
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("the case's transaction: %v", err)
		}
	})
}

// outbox is what has been published in this transaction, in order.
func outbox(t *testing.T, tx db.Tx[db.Tenant]) []string {
	t.Helper()
	var names []string
	err := tx.DB().Table("platformkit_outbox").Order("created_at, id").Pluck("name", &names).Error
	if err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	return names
}

// TestOneTenantsNotificationsAreNotAnothers: the same person id in two tenants
// is two sets of rows, and neither transaction can see the other's — by the
// policy in migrations/000011 and not by anything this module wrote.
func TestOneTenantsNotificationsAreNotAnothers(t *testing.T) {
	_, conn := dbtest.Schema(t)
	svc := internal.NewService(directory{})
	ada := notificationtest.Ada

	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: ada, Title: "acme's news"})
		if err != nil {
			return err
		}
		id = row.ID
		return nil
	})
	if err != nil {
		t.Fatalf("notify in acme: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, total, err := svc.ListFor(ctx, tx, ada, crud.Query{}); err != nil || total != 0 {
			t.Errorf("globex sees %d of acme's notifications (%v)", total, err)
		}
		if _, err := svc.MarkRead(ctx, tx, id, ada); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("globex marked acme's notification read: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("list in globex: %v", err)
	}
}

// TestTheWorkerReadsTheRowBackAndSendsTheMail is the other half of the shape:
// Notify publishes two identifiers and this is what turns them into a message.
//
// Two identifiers, because the payload of an event is copied into the audit
// trail and kept in the outbox for a week — see contracts.EmailRequested — so
// the row, the address and the host are all read here, in the transaction the
// kernel opened in the event's own tenant. The link becomes an absolute URL on
// that tenant's own host, because a mail client has no base to resolve a path
// against and one customer's people must not be sent to another's front door.
func TestTheWorkerReadsTheRowBackAndSendsTheMail(t *testing.T) {
	_, conn := dbtest.Schema(t)
	svc := internal.NewService(directory{})
	box := notification.NewMailbox()
	sub := internal.SendMail(box, directory{}, hosts{}, true)
	if sub.Module != "notification" || sub.Name != contracts.EventEmailRequested {
		t.Fatalf("the subscription is %s to %s", sub.Module, sub.Name)
	}

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Notify(ctx, tx, contracts.Notice{
			Recipient: notificationtest.Ada, Title: "A task was assigned to you",
			Body: "Chiller-2 supply temperature out of band", Link: "/admin/task/tasks/1", Email: true,
		})
		if err != nil {
			return err
		}
		// The event as the relay would hand it over: two ids and a time.
		payload := `{"notificationId":"` + row.ID.String() + `","recipientId":"` + row.RecipientID.String() + `"}`
		if err := sub.Handler(ctx, tx, events.Event{
			ID: uuid.New(), Name: contracts.EventEmailRequested, Payload: []byte(payload),
		}); err != nil {
			return err
		}

		sent := box.Sent()
		if len(sent) != 1 {
			t.Fatalf("the mailbox holds %d messages, want one", len(sent))
		}
		switch msg := sent[0]; {
		case msg.To != notificationtest.AdaEmail:
			t.Errorf("sent to %q", msg.To)
		case msg.Subject != "A task was assigned to you":
			t.Errorf("subject is %q", msg.Subject)
		case !strings.Contains(msg.Body, "Chiller-2 supply temperature out of band"):
			t.Errorf("the body does not carry the notice: %q", msg.Body)
		case !strings.Contains(msg.Body, "https://acme.example.com/admin/task/tasks/1"):
			t.Errorf("the link is not the tenant's own absolute URL: %q", msg.Body)
		}

		// A notice deleted between the request and the send is a skip and a log
		// line, not a failure the outbox retries five times and dead-letters.
		gone := `{"notificationId":"` + uuid.NewString() + `","recipientId":"` + row.RecipientID.String() + `"}`
		if err := sub.Handler(ctx, tx, events.Event{
			ID: uuid.New(), Name: contracts.EventEmailRequested, Payload: []byte(gone),
		}); err != nil || len(box.Sent()) != 1 {
			t.Errorf("a request for a deleted notice = %v and %d messages", err, len(box.Sent()))
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("the transaction: %v", err)
	}
}

// TestTheOutboxCarriesNoMessage. It is the reason the payload is two
// identifiers: an outbox row is kept for a week and modules/audit copies every
// one of them into a table an administrator can read, so an address or a body
// in a payload is an address or a body in the audit trail — and a set-password
// link in one would be a live credential there.
func TestTheOutboxCarriesNoMessage(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	svc := internal.NewService(directory{})
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Notify(ctx, tx, contracts.Notice{
			Recipient: notificationtest.Ada, Title: "Reset your password",
			Body: "Follow the link", Link: "/auth/reset?token=s3cr3t", Email: true,
		})
		return err
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	var payload string
	err = admin.QueryRowContext(t.Context(),
		`SELECT payload::text FROM platformkit_outbox WHERE name = $1`, contracts.EventEmailRequested).Scan(&payload)
	if err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	for _, leaked := range []string{notificationtest.AdaEmail, "s3cr3t", "Follow the link", "Reset your password"} {
		if strings.Contains(payload, leaked) {
			t.Errorf("the mail request carries %q: %s", leaked, payload)
		}
	}
}

// hosts is the HostLookup the worker needs: one tenant, one host. The real one
// is an adapter over the tenant module in apps/platformkit, reading under the
// tenant's own policy.
type hosts struct{}

func (hosts) PublicHost(context.Context, db.Tx[db.Tenant]) (string, error) {
	return "acme.example.com", nil
}
