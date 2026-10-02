package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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

// carries is a provider of one channel — the stub the brief allows ("Push
// adapters may be stubs that record to the ledger"). It answers, so the only
// thing missing can be the thing that asks it.
type carries struct {
	got []contracts.Delivery
}

func (c *carries) Channel() contracts.Channel { return contracts.ChannelPush }

func (c *carries) Deliver(_ context.Context, _ db.Tx[db.Tenant], d contracts.Delivery) error {
	c.got = append(c.got, d)
	return nil
}

// subscriptionNamed is the module's own answer to "who carries this channel":
// the manifest's subscriptions are the only way a channel reaches a worker, so
// a deployment that wires a carrier and composes the module gets one named here
// or the channel it accepted has no terminal row and no future attempt.
func subscriptionNamed(t *testing.T, name string, subs []events.Subscription) events.Subscription {
	t.Helper()
	for _, s := range subs {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("the manifest carries no subscription for %s, so a notice that asked for that channel "+
		"leaves a requested row no worker will ever close and delivery_ledger_coverage is below 1.0 "+
		"for every deployment that wires a provider (subscriptions: %v)", name, subscriptionNames(subs))
	return events.Subscription{}
}

func subscriptionNames(subs []events.Subscription) []string {
	out := make([]string, 0, len(subs))
	for _, s := range subs {
		out = append(out, s.Name)
	}
	return out
}

// TestAChannelTheDeploymentWiresACarrierForReachesATerminalRow is the brief's
// second item read against the ledger it has to leave: a deployment that composes
// a carrier for a channel must be able to account for that channel the way the
// mail path does — the module asks the provider, and writes sent, failed or
// suppressed around the answer. Today Deps.Providers only widens the channels
// contracts.Decide will choose, Notify publishes the channel's event, and nothing
// in the module subscribes to it: the requested row stands open forever, the
// outbox row has no consumer, and the ratio the brief ends on drops below 1.0
// exactly when a provider is wired for a channel beyond mail.
func TestAChannelTheDeploymentWiresACarrierForReachesATerminalRow(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	carrier := &carries{}
	svc, mod := notification.Module(notification.Deps{
		Recipients: directory{},
		Mailer:     notification.NewMailbox(),
		Providers:  contracts.Providers{contracts.ChannelPush: carrier},
	})
	send := subscriptionNamed(t, contracts.EventPushRequested, mod.Subscriptions)

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Notify(ctx, tx, contracts.Notice{
			Recipient: notificationtest.Ada, Title: "the device wants this", Wants: contracts.WantsPush,
		})
		if err != nil {
			return err
		}
		if got, want := ledger(t, tx, row.ID), []string{"in_app requested", "in_app sent", "push requested"}; !slices.Equal(got, want) {
			t.Errorf("the ledger of a notice that asked for push is %v, want %v", got, want)
		}
		body, err := json.Marshal(contracts.DeliveryRequested{NotificationID: row.ID, Recipient: row.RecipientID, At: db.Now()})
		if err != nil {
			return err
		}
		if err := send.Handler(ctx, tx, events.Event{ID: uuid.New(), Name: contracts.EventPushRequested, Payload: body}); err != nil {
			t.Errorf("the push worker refused the delivery: %v", err)
			return errRollback
		}
		if len(carrier.got) != 1 || carrier.got[0].Channel != contracts.ChannelPush || carrier.got[0].NotificationID != row.ID {
			t.Errorf("the carrier this deployment wired was asked to deliver %+v, want one push for %s", carrier.got, row.ID)
		}
		if got, want := ledger(t, tx, row.ID), []string{"in_app requested", "in_app sent", "push requested", "push sent"}; !slices.Equal(got, want) {
			t.Errorf("after its carrier answered, the ledger is %v, want %v", got, want)
		}
		requested, terminal, err := internal.Coverage(tx)
		if err != nil {
			return err
		}
		if requested != terminal {
			t.Errorf("delivery_ledger_coverage is %d terminal of %d requested, want every requested channel closed", terminal, requested)
		}
		return errRollback
	})
	if err != nil && !errors.Is(err, errRollback) {
		t.Fatalf("a notice with a wired carrier: %v", err)
	}
}
