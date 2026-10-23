package events

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Consume subscribes handlers in order, refusing a missing handler or invalid app.
// Each delivery claims and handles the event in its tenant transaction; misplaced
// copies write no claim. Terminal failures use the subscription's terminal hook.
func Consume(ctx context.Context, conn *db.Conn, t Transport, subs []Subscription) error {
	for _, s := range subs {
		if s.Handler == nil {
			return fmt.Errorf("events: subscription %s to %s has no handler", s.Module, s.Name)
		}
		if s.App.Named() {
			// The type is not the check: a Name built by conversion bypasses Parse,
			// and a slug a subject token cannot hold forms an unscoped durable —
			// somebody else's consumer.
			if _, err := appname.Parse(string(s.App)); err != nil {
				return fmt.Errorf("events: subscription %s to %s names app %q: %w", s.Module, s.Name, string(s.App), err)
			}
		}
		durable := s.durable()
		sink := subscriptionSink(conn, s)
		if err := t.Subscribe(ctx, durable, s.Name, sink); err != nil {
			return fmt.Errorf("events: subscribe %s to %s: %w", s.Module, s.Name, err)
		}
	}
	return nil
}

func subscriptionSink(conn *db.Conn, s Subscription) Sink {
	durable := s.durable()
	// Scoped subscriptions also consult the legacy durable that placement renamed.
	older := ""
	if s.App.Named() {
		older = appname.Durable("", s.Module, s.Name)
	}
	return Sink{
		Handle: func(ctx context.Context, ev Event) error {
			return deliverSubscription(ctx, conn, s, ev, durable, older)
		},
		Dead: func(ctx context.Context, ev Event, cause error) error {
			if s.Terminal != nil {
				return terminalLetter(ctx, conn, ev, durable, cause, s.App, s.Terminal)
			}
			return deadLetter(ctx, conn, ev, durable, cause)
		},
	}
}

func deliverSubscription(ctx context.Context, conn *db.Conn, s Subscription, ev Event, durable, older string) error {
	// Bound handler work and duplicate-claim waits; a redelivery waits for the
	// first attempt's claim to commit or roll back.
	ctx, cancel := context.WithTimeout(ctx, handlerTimeout)
	defer cancel()
	holds, err := holdsTenant(ctx, conn, s.App, ev.TenantID)
	if err != nil {
		// Nothing is known about the delivery, so nothing runs and the
		// transport retries: the outbox row stays pending and the event
		// is not lost to a database that is merely unreadable.
		return err
	}
	if !holds {
		// A misplaced copy is acknowledged without a claim; the owning app can
		// still handle its own copy of this event.
		slog.ErrorContext(ctx, "events: delivery names a tenant this app does not hold",
			"app", s.App.String(), "durable", durable, "event", ev.Name,
			"id", ev.ID, "tenant", ev.TenantID)
		return nil
	}
	// Only the id is known here. It is all kit/db needs to scope
	// the transaction, and it is what row-level security reads.
	if err := transport.ValidateOperation(ev.Operation); err != nil {
		return err
	}
	ctx = request.WithOperation(ctx, ev.Operation)
	request.DeferOperation(ctx)
	ctx = tenancy.WithTenant(ctx, tenancy.Tenant{ID: ev.TenantID})
	// One span per delivery, on the trace of the work that published the
	// event rather than of the worker that woke up. See trace.go.
	ctx, span := startDelivery(ctx, ev)
	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return handleClaim(ctx, tx, s, ev, durable, older)
	})
	endSpan(span, err)
	return err
}

func handleClaim(ctx context.Context, tx db.Tx[db.Tenant], s Subscription, ev Event, durable, older string) error {
	// Only unscoped ownership can change after the outer read. Lock and recheck
	// it before claiming; a tenant already assigned to an app cannot leave it.
	if !s.App.Named() {
		holds, err := holdsUnscoped(ctx, tx, ev.TenantID)
		if err != nil {
			return err
		}
		if !holds {
			// Placement won the race before the claim; leave the event to its new app.
			slog.ErrorContext(ctx, "events: delivery's tenant took an app while it was being read",
				"durable", durable, "event", ev.Name,
				"id", ev.ID, "tenant", ev.TenantID)
			return nil
		}
	}
	first, err := claim(tx, ev.ID, durable, older)
	if err != nil || !first {
		return err
	}
	ctx = context.WithValue(ctx, commandDeliveryKey{}, commandDelivery{event: ev, durable: durable})
	return s.Handler(ctx, tx, ev)
}
