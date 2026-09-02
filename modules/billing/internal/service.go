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
		// The first one. The first period is a trial: this module serves it
		// before it asks for anything, and the first renewal is the charge.
		sub = &contracts.Subscription{
			PlanID: planID, Status: contracts.StatusTrial,
			CurrentPeriodStart: now, CurrentPeriodEnd: contracts.Advance(now, plan.Interval),
		}
		if err := crud.Create(ctx, tx, sub); err != nil {
			return nil, err
		}
		return sub, publishSubscribed(ctx, tx, sub, plan, now)
	// Already on this plan with nothing pending: the same intention twice.
	case sub.PlanID == planID && sub.Status != contracts.StatusCancelled && !sub.CancelAtPeriodEnd:
		return sub, nil
	}
	sub.PlanID, sub.CancelAtPeriodEnd = planID, false
	columns := []string{"plan_id", "cancel_at_period_end", "updated_at"}
	if sub.Status == contracts.StatusCancelled {
		// It ended, so there is no period to keep: this is a new one.
		sub.Status = contracts.StatusTrial
		sub.CurrentPeriodStart, sub.CurrentPeriodEnd = now, contracts.Advance(now, plan.Interval)
		columns = append(columns, "status", "current_period_start", "current_period_end")
	}
	if err := crud.Update(ctx, tx, sub, columns...); err != nil {
		return nil, err
	}
	return sub, publishSubscribed(ctx, tx, sub, plan, now)
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
	plan, err := s.planOf(tx, sub)
	if err != nil {
		return nil, nil, err
	}
	if plan.PriceCents == 0 {
		// Nobody to ask, so no second transaction to ask them in.
		return advance(ctx, tx, sub, plan, "", now)
	}
	return sub, &contracts.Charge{
		Subscription: sub.ID, PlanCode: plan.Code, AmountCents: plan.PriceCents,
		Currency: plan.Currency, PeriodEnd: sub.CurrentPeriodEnd,
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
	if sub.ID != c.Subscription || !sub.CurrentPeriodEnd.Equal(c.PeriodEnd) ||
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
	if sub.Status == contracts.StatusPastDue {
		return sub, nil // said last night, and the night before
	}
	sub.Status = contracts.StatusPastDue
	if err := crud.Update(ctx, tx, sub, "status", "updated_at"); err != nil {
		return nil, err
	}
	return sub, events.Publish(ctx, tx, contracts.EventPastDue, contracts.PastDue{
		SubscriptionID: sub.ID, AmountCents: c.AmountCents, Currency: c.Currency,
		Since: c.PeriodEnd, At: now,
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
func advance(ctx context.Context, tx db.Tx[db.Tenant], sub *contracts.Subscription,
	plan *contracts.Plan, receipt string, now time.Time,
) (*contracts.Subscription, *contracts.Charge, error) {
	sub.CurrentPeriodStart = sub.CurrentPeriodEnd
	sub.CurrentPeriodEnd = contracts.Advance(sub.CurrentPeriodStart, plan.Interval)
	sub.Status = contracts.StatusActive
	if err := crud.Update(ctx, tx, sub, "status", "current_period_start", "current_period_end", "updated_at"); err != nil {
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
// still on is not one an administrator may remove. It is why the subscriptions
// table needs no foreign key — a key would refuse the delete of any plan a row
// had ever named, cancelled ones included, and would say so as a constraint
// name. It runs inside the request's transaction, so the refusal rolls the
// delete back.
func RefuseWhileSubscribed(_ context.Context, tx db.Tx[db.Tenant], plan *contracts.Plan) error {
	var live int64
	err := tx.DB().Model(&contracts.Subscription{}).
		Where("plan_id = ? AND status <> ? AND deleted_at IS NULL", plan.ID, contracts.StatusCancelled).
		Count(&live).Error
	if err != nil {
		return err
	}
	if live > 0 {
		return fmt.Errorf("%w: plan %s is still being billed; deactivate it instead, which stops new subscriptions and leaves the ones already on it running",
			crud.ErrConflict, plan.Code)
	}
	return nil
}
