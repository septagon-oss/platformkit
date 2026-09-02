// Package contracts is everything another module, an app or a test may know
// about billing: the two entities, the events, the permissions, the payment
// provider this module needs somebody else to satisfy, and the Service
// interface. The implementation is in ../internal.
//
// A tenant is the customer, so its subscription is a singleton: there is one
// row or there is none. That is why Subscribe takes a plan and no subscription
// id, and why the routes are a resource rather than a collection. A product
// that bills its users rather than its tenants is a different module.
package contracts

import (
	"context"
	"database/sql/driver"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// The two intervals a plan may bill on. There is no day and no week: an
// interval nobody sells is one every renewal has to branch on.
const (
	IntervalMonth = "month"
	IntervalYear  = "year"
)

// The lifecycle. A subscription starts in trial — the first period is served
// before any money is asked for — becomes active once a charge settles,
// past_due when one does not, and cancelled when the customer says so.
const (
	StatusTrial     = "trial"
	StatusActive    = "active"
	StatusPastDue   = "past_due"
	StatusCancelled = "cancelled"
)

// The two closed sets, spelled here as well as in the enum tags below: a tag is
// what the schema reads and this is what Validate reads, and nothing derives
// one from the other.
var (
	intervals = []string{IntervalMonth, IntervalYear}
	statuses  = []string{StatusTrial, StatusActive, StatusPastDue, StatusCancelled}
)

// Features is the feature names a plan includes, one text[] column. It is a
// named type so the array codec is written once, the way modules/user spells
// Roles; both delegate to lib/pq, which the program already links.
type Features []string

func (f Features) Value() (driver.Value, error) { return pq.StringArray(f).Value() }
func (f *Features) Scan(src any) error          { return (*pq.StringArray)(f).Scan(src) }

// Plan is one thing a tenant can subscribe to. The price is an integer of the
// currency's minor unit: money in a float is wrong, money in a decimal is a
// dependency and a serialization question, and money in cents is an int64 every
// language and every database agrees about.
type Plan struct {
	crud.Base

	// Code is what an invoice, a price page and an integration call this plan,
	// unique within the tenant and unchanged when somebody renames it.
	Code string `json:"code" gorm:"type:varchar(60);not null" validate:"required" minLength:"1" maxLength:"60" doc:"Stable identifier, unique within the tenant" example:"pro-monthly"`
	// Name is what a price page shows.
	Name string `json:"name" gorm:"type:varchar(120);not null" validate:"required" minLength:"1" maxLength:"120" doc:"Display name" example:"Pro, billed monthly"`

	// PriceCents is one interval's price in the minor unit of Currency. Zero is
	// free, and a free plan renews without asking a provider for anything.
	PriceCents int64  `json:"priceCents" gorm:"not null;default:0" minimum:"0" doc:"Price of one interval, in the currency's minor unit" default:"0" required:"false" example:"2900"`
	Currency   string `json:"currency" gorm:"type:char(3);not null" validate:"required" minLength:"3" maxLength:"3" doc:"ISO 4217 code" example:"EUR"`
	// Interval is a closed set; the enum tag is what a form renders as a select.
	Interval string `json:"interval" gorm:"type:varchar(10);not null;default:'month'" enum:"month,year" ui:"widget:select" doc:"How long one period lasts" default:"month" required:"false"`

	// Features are names and not permissions: what a name entitles somebody to
	// is the consuming module's business, so a plan can be re-priced without
	// touching an authorization.
	Features Features `json:"features,omitempty" gorm:"type:text[];not null;default:'{}'" required:"false" doc:"Feature names this plan includes"`

	// Active gates enrollment and not existence: a deactivated plan refuses new
	// subscriptions and the ones already on it keep running, because a price
	// somebody agreed to is not something a price-list edit may withdraw.
	//
	// It is required because it could not be defaulted. A Go bool has no third
	// state, so false and absent are one value, and both layers below guess:
	// GORM leaves a zero-valued field out of an INSERT when its tag declares a
	// default, and huma fills a zero-valued property from the schema's. Declare
	// true on either and {"active": false} stores an active plan. The
	// alternative is a *bool in every use of the field to fix one write.
	Active bool `json:"active" gorm:"not null" ui:"widget:checkbox" doc:"Whether this plan accepts new subscriptions" example:"true"`
}

// TableName pins the table, so the entity and migrations/000016 agree.
func (Plan) TableName() string { return "billing_plans" }

// Validate is the entity's own check, run by kit/crud on every write whichever
// door it came through. It normalises as well as refuses: a code that differs
// only in case is the same plan, and two callers must not disagree.
func (p *Plan) Validate(context.Context) error {
	p.Code = strings.ToLower(strings.TrimSpace(p.Code))
	p.Name = strings.TrimSpace(p.Name)
	p.Currency = strings.ToUpper(strings.TrimSpace(p.Currency))
	if p.Interval == "" {
		p.Interval = IntervalMonth
	}
	switch {
	case p.Code == "":
		return fmt.Errorf("a plan needs a code")
	case p.Name == "":
		return fmt.Errorf("a plan needs a name")
	case len(p.Currency) != 3:
		return fmt.Errorf("currency %q is not an ISO 4217 code", p.Currency)
	case p.PriceCents < 0:
		return fmt.Errorf("a price is not negative")
	case !slices.Contains(intervals, p.Interval):
		return fmt.Errorf("interval %q is not %s or %s", p.Interval, IntervalMonth, IntervalYear)
	}
	for _, f := range p.Features {
		if strings.TrimSpace(f) == "" {
			return fmt.Errorf("a feature needs a name")
		}
	}
	return nil
}

// Advance is the end of the period following one that ended at from. Calendar
// arithmetic and not a number of days: a monthly plan bought on the 31st is
// billed on the 28th in February, and nobody means "every 30 days".
func Advance(from time.Time, interval string) time.Time {
	if interval == IntervalYear {
		return from.AddDate(1, 0, 0)
	}
	return from.AddDate(0, 1, 0)
}

// Subscription is the tenant's enrollment in a plan. There is at most one, and
// migrations/000016 says so with a unique index on the tenant.
type Subscription struct {
	crud.Base

	// PlanID is the plan being billed, and a plan of this tenant: every query
	// here runs under the tenant's own policy.
	PlanID uuid.UUID `json:"planId" gorm:"type:uuid;not null" format:"uuid" doc:"The plan this subscription is for"`
	// Status is a closed set, moved only by the commands.
	Status string `json:"status" gorm:"type:varchar(20);not null;default:'trial'" enum:"trial,active,past_due,cancelled" ui:"widget:select" doc:"Lifecycle state" default:"trial" required:"false"`

	// The period being served. The next one starts exactly where this one
	// ended, so periods chain rather than drift with the hour a job ran.
	CurrentPeriodStart time.Time `json:"currentPeriodStart" gorm:"type:timestamptz;not null" ui:"widget:datetime" doc:"Start of the period being served" required:"false" readOnly:"true"`
	CurrentPeriodEnd   time.Time `json:"currentPeriodEnd" gorm:"type:timestamptz;not null" ui:"widget:datetime" doc:"End of the period being served" required:"false" readOnly:"true"`

	// CancelAtPeriodEnd is a customer who has left and is still owed what they
	// paid for. Nothing shortens the period: cancelling is not a refund.
	CancelAtPeriodEnd bool `json:"cancelAtPeriodEnd" gorm:"not null;default:false" ui:"widget:checkbox" doc:"Whether this subscription ends when the current period does" default:"false" required:"false"`
}

// TableName pins the table, so the entity and migrations/000016 agree.
func (Subscription) TableName() string { return "billing_subscriptions" }

// Expired reports whether the period being served has run out.
func (s *Subscription) Expired(now time.Time) bool { return !now.Before(s.CurrentPeriodEnd) }

// Validate is the entity's own check.
func (s *Subscription) Validate(context.Context) error {
	if s.Status == "" {
		s.Status = StatusTrial
	}
	switch {
	case s.PlanID == uuid.Nil:
		return fmt.Errorf("a subscription is to a plan")
	case !slices.Contains(statuses, s.Status):
		return fmt.Errorf("status %q is not a lifecycle state", s.Status)
	case !s.CurrentPeriodEnd.After(s.CurrentPeriodStart):
		return fmt.Errorf("a period ends after it starts")
	}
	return nil
}

// Charge is one attempt to take money for one period. It carries the plan's
// code and the period as well as the amount, so a provider's own record can be
// reconciled against this one.
type Charge struct {
	Subscription uuid.UUID `json:"subscriptionId"`
	PlanCode     string    `json:"planCode"`
	AmountCents  int64     `json:"amountCents"`
	Currency     string    `json:"currency"`
	PeriodEnd    time.Time `json:"periodEnd"`
}

// Receipt is what a provider says about a charge. Settled is the whole of the
// decision — a subscription is either being served or it is past due — and a
// provider that is still thinking says false and is asked again tomorrow.
type Receipt struct {
	// Reference is the provider's own identifier, kept so a payment can be
	// found in somebody else's system.
	Reference string    `json:"reference"`
	Settled   bool      `json:"settled"`
	At        time.Time `json:"at"`
}

// PaymentProvider takes money. There is one implementation here, Manual, which
// records what is owed and moves nothing; the ones that speak to a payment
// processor live outside this repository, because a reference architecture
// carrying a Stripe client would be teaching Stripe.
//
// It takes no transaction, and that is the shape of the whole renewal below: a
// charge is a call to somebody else's machine.
type PaymentProvider interface {
	Charge(ctx context.Context, c Charge) (Receipt, error)
}

// Service is the subscription lifecycle: the transitions generic CRUD cannot
// safely infer, and the one read that finds the singleton.
//
// Every command takes the caller's transaction rather than opening one, so the
// state change and its event commit together. The errors are kit/crud's:
// ErrNotFound for a tenant with no subscription or a plan that is not there,
// ErrConflict for a state that refuses the command, ErrInvalid for an argument
// it cannot use. Each command is idempotent when repeated with the same
// argument: the callers that retry — a browser, a nightly job — must not each
// produce an event.
type Service interface {
	// Current is the tenant's subscription, or ErrNotFound when it has never
	// had one.
	Current(ctx context.Context, tx db.Tx[db.Tenant]) (*Subscription, error)

	// Subscribe enrolls the tenant in a plan. The same plan again says nothing;
	// a different one swaps the plan and leaves the period alone, so the new
	// price applies at the next renewal and nobody is refunded or
	// double-charged for days already served — this module does not prorate.
	// An inactive plan is refused, and a subscription that has ended is
	// resubscribed rather than duplicated, because a tenant has one.
	Subscribe(ctx context.Context, tx db.Tx[db.Tenant], planID uuid.UUID) (*Subscription, error)

	// Cancel ends the subscription: at the end of the period it is serving, or
	// now. Cancelling twice the same way changes nothing; ending it now after
	// asking to end later is a second decision and says so; asking to end later
	// after it has ended is a conflict.
	Cancel(ctx context.Context, tx db.Tx[db.Tenant], atPeriodEnd bool) (*Subscription, error)

	// Renew advances a subscription whose period has run out, and it is the
	// half of a renewal where no money moves: a period still running is left
	// alone, a subscription whose customer cancelled is ended — silently,
	// because Cancel already said when it would — and a free plan starts its
	// next period and publishes billing.renewed. Otherwise it returns the
	// charge and changes nothing; the caller takes that money outside every
	// transaction and brings the answer to Settle.
	Renew(ctx context.Context, tx db.Tx[db.Tenant]) (*Subscription, *Charge, error)

	// Settle records what a provider said. A settled receipt starts the next
	// period and publishes billing.renewed; an unsettled one leaves the period
	// alone, marks the subscription past due and publishes billing.past_due —
	// once, because the job runs every night and a customer whose card is dead
	// must not be an event a night forever. A charge for a period the
	// subscription has moved past is ignored and says nothing.
	Settle(ctx context.Context, tx db.Tx[db.Tenant], c Charge, r Receipt) (*Subscription, error)
}
