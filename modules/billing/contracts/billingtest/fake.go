package billingtest

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/billing/contracts"
)

// Fake is contracts.Service over two maps: the same rules, no database, no
// transaction. A consumer that wants to test what it does when a tenant
// subscribes takes one of these instead of a Postgres.
//
// It ignores the transaction it is handed, and that is the honest limit of it:
// it cannot tell a caller that a write did not commit, because nothing here
// commits. Everything it can be wrong about is what RunService checks, and
// fake_test.go runs the whole suite against it.
type Fake struct {
	mu    sync.Mutex
	plans map[uuid.UUID]contracts.Plan
	// sub is the tenant's one subscription, nil until there is one: the fake
	// stands in for one tenant, which is the scope a tenant transaction has.
	sub       *contracts.Subscription
	published []string
}

// NewFake returns an empty store.
func NewFake() *Fake { return &Fake{plans: map[uuid.UUID]contracts.Plan{}} }

var _ contracts.Service = (*Fake)(nil)

// Put stores a plan, giving it an id if it has none, and returns the id. It is
// the fake's stand-in for the plan create route.
func (f *Fake) Put(plan *contracts.Plan) uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	if plan.ID == uuid.Nil {
		plan.ID = uuid.New()
	}
	if plan.Interval == "" {
		plan.Interval = contracts.IntervalMonth
	}
	f.plans[plan.ID] = *plan
	return plan.ID
}

// Expire moves the subscription's period into the past, so a test can renew
// without waiting a month. It is the fake's stand-in for the clock.
func (f *Fake) Expire() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sub == nil {
		return
	}
	now := db.Now()
	f.sub.CurrentPeriodStart = now.AddDate(0, -1, -1)
	f.sub.CurrentPeriodEnd = now.Add(-24 * time.Hour)
}

// Published is the names of the events the fake would have emitted.
func (f *Fake) Published() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.published)
}

// Plans is every plan the fake holds, for a consumer asserting on state.
func (f *Fake) Plans() map[uuid.UUID]contracts.Plan {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.plans)
}

// Current mirrors internal.Service.Current.
func (f *Fake) Current(context.Context, db.Tx[db.Tenant]) (*contracts.Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current()
}

// Subscribe mirrors internal.Service.Subscribe.
func (f *Fake) Subscribe(_ context.Context, _ db.Tx[db.Tenant], planID uuid.UUID) (*contracts.Subscription, error) {
	if planID == uuid.Nil {
		return nil, fmt.Errorf("%w: a subscription is to a plan", crud.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	plan, ok := f.plans[planID]
	if !ok {
		return nil, crud.ErrNotFound
	}
	if !plan.Active {
		return nil, fmt.Errorf("%w: plan %s does not accept new subscriptions", crud.ErrConflict, plan.Code)
	}
	now := db.Now()
	if f.sub == nil {
		f.sub = &contracts.Subscription{
			Base:   crud.Base{ID: uuid.New()},
			PlanID: planID, Status: contracts.StatusTrial,
			CurrentPeriodStart: now, CurrentPeriodEnd: contracts.Advance(now, plan.Interval),
		}
		return f.commit(contracts.EventSubscribed), nil
	}
	if f.sub.PlanID == planID && f.sub.Status != contracts.StatusCancelled && !f.sub.CancelAtPeriodEnd {
		return f.copy(), nil
	}
	f.sub.PlanID, f.sub.CancelAtPeriodEnd = planID, false
	if f.sub.Status == contracts.StatusCancelled {
		f.sub.Status = contracts.StatusTrial
		f.sub.CurrentPeriodStart, f.sub.CurrentPeriodEnd = now, contracts.Advance(now, plan.Interval)
	}
	return f.commit(contracts.EventSubscribed), nil
}

// Cancel mirrors internal.Service.Cancel.
func (f *Fake) Cancel(_ context.Context, _ db.Tx[db.Tenant], atPeriodEnd bool) (*contracts.Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.current(); err != nil {
		return nil, err
	}
	if atPeriodEnd {
		switch {
		case f.sub.Status == contracts.StatusCancelled:
			return nil, fmt.Errorf("%w: this subscription has already ended", crud.ErrConflict)
		case f.sub.CancelAtPeriodEnd:
			return f.copy(), nil
		}
		f.sub.CancelAtPeriodEnd = true
		return f.commit(contracts.EventCancelled), nil
	}
	if f.sub.Status == contracts.StatusCancelled {
		return f.copy(), nil
	}
	f.sub.Status, f.sub.CancelAtPeriodEnd = contracts.StatusCancelled, false
	return f.commit(contracts.EventCancelled), nil
}

// Renew mirrors internal.Service.Renew.
func (f *Fake) Renew(_ context.Context, _ db.Tx[db.Tenant]) (*contracts.Subscription, *contracts.Charge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.current(); err != nil {
		return nil, nil, err
	}
	now := db.Now()
	if f.sub.Status == contracts.StatusCancelled || !f.sub.Expired(now) {
		return f.copy(), nil, nil
	}
	if f.sub.CancelAtPeriodEnd {
		f.sub.Status = contracts.StatusCancelled
		return f.copy(), nil, nil
	}
	plan, ok := f.plans[f.sub.PlanID]
	if !ok {
		return nil, nil, crud.ErrNotFound
	}
	if plan.PriceCents == 0 {
		return f.advance(plan), nil, nil
	}
	return f.copy(), &contracts.Charge{
		Subscription: f.sub.ID, PlanCode: plan.Code, AmountCents: plan.PriceCents,
		Currency: plan.Currency, PeriodEnd: f.sub.CurrentPeriodEnd,
	}, nil
}

// Settle mirrors internal.Service.Settle.
func (f *Fake) Settle(_ context.Context, _ db.Tx[db.Tenant], c contracts.Charge, r contracts.Receipt) (*contracts.Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.current(); err != nil {
		return nil, err
	}
	if f.sub.ID != c.Subscription || !f.sub.CurrentPeriodEnd.Equal(c.PeriodEnd) ||
		f.sub.Status == contracts.StatusCancelled {
		return f.copy(), nil
	}
	if r.Settled {
		plan, ok := f.plans[f.sub.PlanID]
		if !ok {
			return nil, crud.ErrNotFound
		}
		return f.advance(plan), nil
	}
	if f.sub.Status == contracts.StatusPastDue {
		return f.copy(), nil
	}
	f.sub.Status = contracts.StatusPastDue
	return f.commit(contracts.EventPastDue), nil
}

// advance is internal.advance: the next period, chained to the last.
func (f *Fake) advance(plan contracts.Plan) *contracts.Subscription {
	f.sub.CurrentPeriodStart = f.sub.CurrentPeriodEnd
	f.sub.CurrentPeriodEnd = contracts.Advance(f.sub.CurrentPeriodStart, plan.Interval)
	f.sub.Status = contracts.StatusActive
	return f.commit(contracts.EventRenewed)
}

// current is the subscription, or the error a tenant without one gets. The
// caller holds the lock.
func (f *Fake) current() (*contracts.Subscription, error) {
	if f.sub == nil {
		return nil, crud.ErrNotFound
	}
	return f.copy(), nil
}

// copy is what a caller is handed, so mutating it does not reach into the
// store — which is what a database would do. The caller holds the lock.
func (f *Fake) copy() *contracts.Subscription {
	out := *f.sub
	return &out
}

// commit stamps the subscription, records the event and returns the copy. The
// caller holds the lock.
func (f *Fake) commit(event string) *contracts.Subscription {
	f.sub.UpdatedAt = db.Now()
	f.published = append(f.published, event)
	return f.copy()
}
