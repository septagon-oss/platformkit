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

// TestTheWorkerRendersAndSendsTheMail is the other half of the shape: Notify
// publishes a request and this is what turns it into a message. The template is
// the module's own, the link becomes an absolute URL because a mail client has
// no base to resolve a path against, and nothing here touches the database.
func TestTheWorkerRendersAndSendsTheMail(t *testing.T) {
	box := notificationtest.NewMailbox()
	sub := internal.SendMail(box, "acme.example.com")
	if sub.Module != "notification" || sub.Name != contracts.EventEmailRequested {
		t.Fatalf("the subscription is %s to %s", sub.Module, sub.Name)
	}

	payload := `{"notificationId":"` + uuid.NewString() + `","recipientId":"` + uuid.NewString() +
		`","to":"ada@acme.example.com","title":"A task was assigned to you",` +
		`"body":"Chiller-2 supply temperature out of band","link":"/admin/task/tasks/1"}`
	err := sub.Handler(t.Context(), db.Tx[db.Tenant]{}, events.Event{
		ID: uuid.New(), Name: contracts.EventEmailRequested, Payload: []byte(payload),
	})
	if err != nil {
		t.Fatalf("the mail subscription: %v", err)
	}

	sent := box.Sent()
	if len(sent) != 1 {
		t.Fatalf("the mailbox holds %d messages, want one", len(sent))
	}
	switch msg := sent[0]; {
	case msg.To != "ada@acme.example.com":
		t.Errorf("sent to %q", msg.To)
	case msg.Subject != "A task was assigned to you":
		t.Errorf("subject is %q", msg.Subject)
	case !strings.Contains(msg.Body, "Chiller-2 supply temperature out of band"):
		t.Errorf("the body does not carry the notice: %q", msg.Body)
	case !strings.Contains(msg.Body, "https://acme.example.com/admin/task/tasks/1"):
		t.Errorf("the link is not absolute: %q", msg.Body)
	}

	// A request with no address is nothing to do, not an error to retry: the
	// row was still written and the person still sees it in the application.
	err = sub.Handler(t.Context(), db.Tx[db.Tenant]{}, events.Event{
		ID: uuid.New(), Name: contracts.EventEmailRequested, Payload: []byte(`{"title":"x"}`),
	})
	if err != nil || len(box.Sent()) != 1 {
		t.Errorf("a request with no address = %v and %d messages", err, len(box.Sent()))
	}
}
