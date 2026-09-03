// Package internal is every implementation of the billing module. Nothing
// outside modules/billing can import it, which is the compiler enforcing idea
// 3: a consumer takes contracts.Service, and taking anything else does not
// build.
package internal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/billing/contracts"
)

// Service is the subscription lifecycle. It has no fields: everything a command
// needs arrives with the transaction it is given, and the one thing it does not
// do is take money — that is the caller's, between two transactions.
type Service struct{}

// NewService returns the lifecycle commands. It takes nothing, on purpose: see
// the type. module.go constructs it.
func NewService() *Service { return &Service{} }

var _ contracts.Service = (*Service)(nil)

// Current is the tenant's one subscription: a query rather than a crud.Get,
// because the caller has no id to give and row-level security has already
// narrowed the table to the one row there is.
func (s *Service) Current(_ context.Context, tx db.Tx[db.Tenant]) (*contracts.Subscription, error) {
	var sub contracts.Subscription
	if err := tx.DB().Where("deleted_at IS NULL").Take(&sub).Error; err != nil {
		return nil, crud.Classify(err)
	}
	return &sub, nil
}

// Subscribe enrolls the tenant in a plan. See contracts.Service.
func (s *Service) Subscribe(ctx context.Context, tx db.Tx[db.Tenant], planID uuid.UUID) (*contracts.Subscription, error) {
	if planID == uuid.Nil {
		return nil, fmt.Errorf("%w: a subscription is to a plan", crud.ErrInvalid)
	}
	plan, err := crud.Get[*contracts.Plan](tx, planID)
	if err != nil {
		return nil, err
	}
	if !plan.Active {
		return nil, fmt.Errorf("%w: plan %s does not accept new subscriptions", crud.ErrConflict, plan.Code)
	}
	now := db.Now()

	sub, err := s.Current(ctx, tx)
	switch {
	case err != nil && !errors.Is(err, crud.ErrNotFound):
		return nil, err
	case err != nil:
		// The first live one. Whether it is a trial is TrialUsedAt's answer and
		// not this row's absence: a subscription that was deleted is still a
		// trial this tenant was served, and the trial is once per tenant, ever.
		used, err := trialUsedAt(tx)
		if err != nil {
			return nil, err
		}
		sub = &contracts.Subscription{
			PlanID: planID, TrialUsedAt: used, AnchorDay: contracts.AnchorOf(now),
			PriceCents: plan.PriceCents, Currency: plan.Currency,
		}
		if used == nil {
			// The trial: this module serves the first period before it asks for
			// anything, and the first renewal is the charge.
			sub.Status, sub.TrialUsedAt = contracts.StatusTrial, &now
			sub.CurrentPeriodStart = now
			sub.CurrentPeriodEnd = contracts.Advance(now, plan.Interval, sub.AnchorDay)
		} else {
			sub.Status = contracts.StatusActive
			sub.CurrentPeriodStart, sub.CurrentPeriodEnd = back(now, plan.Interval), now
		}
		if err := crud.Create(ctx, tx, sub); err != nil {
			return nil, err
		}
		return sub, publishSubscribed(ctx, tx, sub, plan, now)
	// Already on this plan with nothing pending: the same intention twice.
	case sub.PlanID == planID && sub.Status != contracts.StatusCancelled && !sub.CancelAtPeriodEnd:
		return sub, nil
	}
	// A tenant that owes money does not get to choose a cheaper plan. The debt
	// is for a period already served; it is settled, or the grace period runs
	// out and the subscription ends. This is the review's escape route, and it
	// asks Outstanding rather than the status because a subscription cancelled
	// while it owed is cancelled and still owes.
	if end, owes := sub.Outstanding(); owes && sub.PlanID != planID {
		return nil, fmt.Errorf("%w: this subscription owes for the period ending %s; it is settled before the plan changes",
			crud.ErrConflict, end.UTC().Format(time.RFC3339))
	}
	sub.PlanID, sub.CancelAtPeriodEnd = planID, false
	columns := []string{"plan_id", "cancel_at_period_end", "updated_at"}
	if sub.Status == contracts.StatusCancelled {
		// It ended, so it comes back — and it comes back exactly where it left
		// off. The attempt count, the grace clock and an outstanding period are
		// not written here at all, so cancelling and resubscribing is not a way
		// of clearing them: a review did that live, for as many free periods as
		// it liked. Only a subscription that owes nothing is given a period.
		sub.Status = contracts.StatusPastDue
		columns = append(columns, "status")
		if _, owes := sub.Outstanding(); !owes {
			// Not a second trial — the trial is once per tenant — so the period
			// it is given is one that has already ended, and the next renewal
			// charges for the period it is about to serve rather than serving
			// it free.
			sub.Status = contracts.StatusActive
			sub.CurrentPeriodStart, sub.CurrentPeriodEnd = back(now, plan.Interval), now
			sub.PriceCents, sub.Currency = plan.PriceCents, plan.Currency
			sub.AnchorDay = contracts.AnchorOf(now)
			columns = append(columns, "current_period_start", "current_period_end",
				"price_cents", "currency", "anchor_day")
		}
	}
	if err := crud.Update(ctx, tx, sub, columns...); err != nil {
		return nil, err
	}
	return sub, publishSubscribed(ctx, tx, sub, plan, now)
}

// trialUsedAt is when this tenant was issued its one trial, or nil when it
// never was. It reads every row and not the live one, soft-deleted included,
// because "once per tenant, ever" is a fact about the tenant and a subscription
// that was removed is still a trial that was served.
func trialUsedAt(tx db.Tx[db.Tenant]) (*time.Time, error) {
	var at []time.Time
	err := tx.DB().Model(&contracts.Subscription{}).Where("trial_used_at IS NOT NULL").
		Order("trial_used_at").Limit(1).Pluck("trial_used_at", &at).Error
	if err != nil {
		return nil, crud.Classify(err)
	}
	if len(at) == 0 {
		return nil, nil
	}
	return &at[0], nil
}

// back is one interval before now, for the period a resubscriber is given: it
// has already ended, so the next renewal charges for the one that follows it.
func back(now time.Time, interval string) time.Time {
	if interval == contracts.IntervalYear {
		return now.AddDate(-1, 0, 0)
	}
	return now.AddDate(0, -1, 0)
}

// Cancel ends the subscription, now or at the end of the period. See
// contracts.Service.
func (s *Service) Cancel(ctx context.Context, tx db.Tx[db.Tenant], atPeriodEnd bool) (*contracts.Subscription, error) {
	sub, err := s.Current(ctx, tx)
	if err != nil {
		return nil, err
	}
	at := db.Now()
	if atPeriodEnd {
		switch {
		case sub.Status == contracts.StatusCancelled:
			return nil, fmt.Errorf("%w: this subscription has already ended", crud.ErrConflict)
		case sub.CancelAtPeriodEnd:
			return sub, nil
		}
		sub.CancelAtPeriodEnd = true
		if err := crud.Update(ctx, tx, sub, "cancel_at_period_end", "updated_at"); err != nil {
			return nil, err
		}
		return sub, events.Publish(ctx, tx, contracts.EventCancelled, contracts.Cancelled{
			SubscriptionID: sub.ID, EndsAt: sub.CurrentPeriodEnd, At: at,
		})
	}
	if sub.Status == contracts.StatusCancelled {
		return sub, nil
	}
	// The dunning ceiling's other escape route. Ending it now used to take the
	// subscription out of past_due, and resubscribing gave it a fresh period; a
	// review ran that loop live. A customer who owes for a period already served
	// may leave at the end of the one they are in, and may not walk away from
	// the debt by leaving in the middle.
	if end, owes := sub.Outstanding(); owes {
		return nil, fmt.Errorf("%w: this subscription owes for the period ending %s; it is settled, or the grace period runs out, before it ends now — cancel at period end instead",
			crud.ErrConflict, end.UTC().Format(time.RFC3339))
	}
	// The period is left where it is: what was paid for is a fact, and the
	// status is what says service has stopped.
	sub.Status, sub.CancelAtPeriodEnd = contracts.StatusCancelled, false
	if err := crud.Update(ctx, tx, sub, "status", "cancel_at_period_end", "updated_at"); err != nil {
		return nil, err
	}
	return sub, events.Publish(ctx, tx, contracts.EventCancelled, contracts.Cancelled{
		SubscriptionID: sub.ID, EndsAt: at, Immediate: true, At: at,
	})
}

// Renew is the free half of a renewal. See contracts.Service.
func (s *Service) Renew(ctx context.Context, tx db.Tx[db.Tenant]) (*contracts.Subscription, *contracts.Charge, error) {
	sub, err := s.Current(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	now := db.Now()
	if sub.Status == contracts.StatusCancelled || !sub.Expired(now) {
		return sub, nil, nil
	}
	if sub.CancelAtPeriodEnd {
		// Silent: Cancel published billing.cancelled with this very moment as
		// its EndsAt, and saying it again is one departure told twice.
		sub.Status = contracts.StatusCancelled
		if err := crud.Update(ctx, tx, sub, "status", "updated_at"); err != nil {
			return nil, nil, err
		}
		return sub, nil, nil
	}
	// The ceiling on dunning. Without it a card that stopped working was
	// retried every night for as long as the installation ran, and the customer
	// was served for as long as it was retried.
	if sub.GraceExpired(now) {
		sub.Status = contracts.StatusCancelled
		if err := crud.Update(ctx, tx, sub, "status", "updated_at"); err != nil {
			return nil, nil, err
		}
		return sub, nil, events.Publish(ctx, tx, contracts.EventCancelled, contracts.Cancelled{
			SubscriptionID: sub.ID, EndsAt: now, Immediate: true,
			Reason: contracts.CancelledByDunning, At: now,
		})
	}
	plan, err := s.planOf(tx, sub)
	if err != nil {
		return nil, nil, err
	}
	// The stamped price, not the plan's. A price list edit applies from the
	// next period, which is what advance below copies forward.
	if sub.PriceCents == 0 {
		// Nobody to ask, so no second transaction to ask them in.
		return advance(ctx, tx, sub, plan, "", now)
	}
	return sub, &contracts.Charge{
		Subject: sub.ID, PlanCode: plan.Code, AmountCents: sub.PriceCents,
		Currency: sub.Currency, PeriodEnd: sub.CurrentPeriodEnd,
		IdempotencyKey: contracts.Key(sub.ID, sub.CurrentPeriodEnd),
	}, nil
}

// Settle records what the provider said. See contracts.Service.
func (s *Service) Settle(ctx context.Context, tx db.Tx[db.Tenant], c contracts.Charge, r contracts.Receipt) (*contracts.Subscription, error) {
	sub, err := s.Current(ctx, tx)
	if err != nil {
		return nil, err
	}
	// The world may have moved between the two transactions. A charge for a
	// period this subscription is no longer in answers no question it has.
	if sub.ID != c.Subject || !sub.CurrentPeriodEnd.Equal(c.PeriodEnd) ||
		sub.Status == contracts.StatusCancelled {
		return sub, nil
	}
	now := db.Now()
	if r.Settled {
		plan, err := s.planOf(tx, sub)
		if err != nil {
			return nil, err
		}
		out, _, err := advance(ctx, tx, sub, plan, r.Reference, now)
		return out, err
	}
	if r.Pending {
		// The provider has the charge and cannot yet say. Neither paid nor
		// past due: the same charge is presented again tomorrow, under the same
		// idempotency key, and the provider is the one that has to make that
		// one payment.
		return sub, nil
	}
	sub.AttemptCount++
	if sub.PastDueSince == nil {
		sub.PastDueSince = &now
	}
	first := sub.Status != contracts.StatusPastDue
	sub.Status = contracts.StatusPastDue
	if err := crud.Update(ctx, tx, sub, "status", "attempt_count", "past_due_since", "updated_at"); err != nil {
		return nil, err
	}
	if !first {
		return sub, nil // said last night, and the night before
	}
	return sub, events.Publish(ctx, tx, contracts.EventPastDue, contracts.PastDue{
		SubscriptionID: sub.ID, AmountCents: c.AmountCents, Currency: c.Currency,
		Since: c.PeriodEnd, Attempt: sub.AttemptCount,
		EndsAt: sub.PastDueSince.Add(contracts.GraceDays * 24 * time.Hour), At: now,
	})
}

// planOf is the plan a subscription is on. The delete route refuses to remove
// one somebody is still on, so a missing plan is a broken installation, and the
// error says which subscription it broke.
func (s *Service) planOf(tx db.Tx[db.Tenant], sub *contracts.Subscription) (*contracts.Plan, error) {
	plan, err := crud.Get[*contracts.Plan](tx, sub.PlanID)
	if err != nil {
		return nil, fmt.Errorf("billing: the plan of subscription %s: %w", sub.ID, err)
	}
	return plan, nil
}

// advance starts the next period, chained to the end of the last, and says so.
//
// It is also where a re-priced plan takes effect: the period just paid for was
// charged at the price stamped on the subscription, and the one starting now is
// stamped with the plan's current price. That is the whole of "re-pricing is
// not retroactive", and the dunning state is cleared here because a period that
// started is a period that was paid for.
func advance(ctx context.Context, tx db.Tx[db.Tenant], sub *contracts.Subscription,
	plan *contracts.Plan, receipt string, now time.Time,
) (*contracts.Subscription, *contracts.Charge, error) {
	sub.CurrentPeriodStart = sub.CurrentPeriodEnd
	sub.CurrentPeriodEnd = contracts.Advance(sub.CurrentPeriodStart, plan.Interval, sub.AnchorDay)
	sub.Status = contracts.StatusActive
	sub.PriceCents, sub.Currency = plan.PriceCents, plan.Currency
	sub.AttemptCount, sub.PastDueSince = 0, nil
	if err := crud.Update(ctx, tx, sub, "status", "current_period_start", "current_period_end",
		"price_cents", "currency", "attempt_count", "past_due_since", "updated_at"); err != nil {
		return nil, nil, err
	}
	return sub, nil, events.Publish(ctx, tx, contracts.EventRenewed, contracts.Renewed{
		SubscriptionID: sub.ID, PeriodStart: sub.CurrentPeriodStart,
		PeriodEnd: sub.CurrentPeriodEnd, Receipt: receipt, At: now,
	})
}

func publishSubscribed(ctx context.Context, tx db.Tx[db.Tenant], sub *contracts.Subscription,
	plan *contracts.Plan, at time.Time,
) error {
	return events.Publish(ctx, tx, contracts.EventSubscribed, contracts.Subscribed{
		SubscriptionID: sub.ID, PlanID: plan.ID, PlanCode: plan.Code,
		Status: sub.Status, PeriodEnd: sub.CurrentPeriodEnd, At: at,
	})
}

// RefuseWhileSubscribed is the plan delete route's hook: a plan somebody is
// still on is not one the operator may remove. It is why the subscriptions
// table needs no foreign key — a key would refuse the delete of any plan a row
// had ever named, cancelled ones included, and would say so as a constraint
// name.
//
// It counts under system access, and that is the consequence of the catalogue
// being shared: the plan being deleted is one every tenant can subscribe to,
// and the operator's own transaction sees only the operator's subscriptions. A
// tenant-scoped count here would have let the operator delete a plan half its
// customers were being billed for and reported nothing.
//
// The count is in a transaction of its own, on a detached context, because a
// system transaction cannot nest inside a tenant one. The refusal still rolls
// the delete back: it is an error returned from a hook that runs inside the
// request's transaction.
func RefuseWhileSubscribed(token tenancy.SystemToken) func(context.Context, db.Tx[db.Tenant], *contracts.Plan) error {
	return func(ctx context.Context, _ db.Tx[db.Tenant], plan *contracts.Plan) error {
		conn, ok := httpx.ConnFrom(ctx)
		if !ok {
			return fmt.Errorf("billing: no connection to count the subscriptions on plan %s", plan.Code)
		}
		var live int64
		err := db.RunSystem(db.Detached(ctx), conn, token, func(_ context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Model(&contracts.Subscription{}).
				Where("plan_id = ? AND status <> ? AND deleted_at IS NULL", plan.ID, contracts.StatusCancelled).
				Count(&live).Error
		})
		if err != nil {
			return err
		}
		if live > 0 {
			return fmt.Errorf("%w: plan %s is still being billed, by %d subscription(s); deactivate it instead, which stops new subscriptions and leaves the ones already on it running",
				crud.ErrConflict, plan.Code, live)
		}
		return nil
	}
}
