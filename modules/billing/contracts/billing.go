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
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"golang.org/x/text/currency"

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

// The bounds a plan is written inside.
//
// MaxPriceCents is a hundred million of the minor unit — a million euros for one
// period — and it is here because an int64 that nothing bounds is a plan
// somebody fat-fingers into an invoice for the national debt, and because the
// column is a bigint that would take it.
const (
	MaxPriceCents = 100_000_000
	MaxCode       = 40
	MaxName       = 120
)

// planCode is what a plan may be called: lower case, digits and single dashes,
// starting with a letter or a digit, two to forty characters. It is the same
// grammar a slug has, and for the same reason — a code appears in a URL, in an
// invoice and in somebody else's integration — and it was unvalidated, so
// "  Pro Monthly!!  " normalised to itself and became a code no integration
// could quote.
var planCode = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,39}$`)

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
	case !planCode.MatchString(p.Code):
		return fmt.Errorf("%q is not a plan code; a code is 2 to %d characters of a-z, 0-9 and dashes, and it appears in a URL and on an invoice", p.Code, MaxCode)
	case p.Name == "":
		return fmt.Errorf("a plan needs a name")
	case utf8.RuneCountInString(p.Name) > MaxName:
		return fmt.Errorf("a plan's name is at most %d characters", MaxName)
	case !ValidCurrency(p.Currency):
		return fmt.Errorf("currency %q is not an ISO 4217 code", p.Currency)
	case p.PriceCents < 0:
		return fmt.Errorf("a price is not negative")
	case p.PriceCents > MaxPriceCents:
		return fmt.Errorf("a price of %d is past the %d this module will bill; a number that large is a typo, and the one time it is not, it is a contract and not a price list", p.PriceCents, MaxPriceCents)
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

// ValidCurrency reports whether code is an ISO 4217 alphabetic code. The check
// was len(code) == 3, so "AAA", "ZZZ" and "EUR" were one answer, and a
// subscription could be stamped with a currency no payment processor has.
//
// The table is golang.org/x/text/currency, which this program already links
// through the modules that format numbers: a hand-written list of 180 codes is
// a list that goes stale the next time a country redenominates.
func ValidCurrency(code string) bool {
	_, err := currency.ParseISO(code)
	return err == nil
}

// Advance is the end of the period following one that ended at from, on anchor,
// the day of the month the subscription bills on.
//
// Calendar arithmetic and not a number of days, and then the correction two
// earlier comments claimed and neither implementation made. time.AddDate
// normalises, so a monthly plan bought on the 31st of January advances to the
// 3rd of March; clamping that back to the 28th of February fixes one month and
// breaks every month after it, because the next period is then computed from
// the 28th and the 31st never comes back.
//
// The anchor is what fixes it: it is carried on the subscription rather than
// read off the period that just ended, so February clamps to the 28th and March
// returns to the 31st. That is what every payment processor does and what the
// customer expects. An anchor of zero means from's own day, which is what a
// subscription written before the column existed has.
func Advance(from time.Time, interval string, anchor int) time.Time {
	years, months := 0, 1
	if interval == IntervalYear {
		years, months = 1, 0
	}
	if anchor <= 0 {
		anchor = from.Day()
	}
	// The first of the month landed in, keeping the time of day: AddDate on the
	// first of a month never normalises, so this is the month that was asked
	// for and not the one an overflow spilled into.
	first := time.Date(from.Year(), from.Month(), 1, from.Hour(), from.Minute(),
		from.Second(), from.Nanosecond(), from.Location()).AddDate(years, months, 0)
	// How many days that month has: the first of the next, less a day.
	return first.AddDate(0, 0, min(anchor, first.AddDate(0, 1, -1).Day())-1)
}

// AnchorOf is the day of the month a subscription starting at t bills on. It is
// the one place the anchor is derived, so the column and Advance agree.
func AnchorOf(t time.Time) int { return t.Day() }

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

	// PriceCents and Currency are the plan's price as it was when this
	// subscription last started a period, stamped here rather than read from
	// the plan at renewal.
	//
	// Without them a price list edit was retroactive: raising a plan's price
	// changed what every live subscriber was charged that night, and changing
	// its currency charged them in another one. Stamping is what makes
	// re-pricing apply from the next period — the renewal charges what is here,
	// and then copies the plan's current price forward for the period after.
	PriceCents int64  `json:"priceCents" gorm:"not null;default:0" ui:"hide:list" doc:"What this subscription is billed per period, in the minor unit of Currency" required:"false" readOnly:"true"`
	Currency   string `json:"currency,omitempty" gorm:"type:varchar(3);not null;default:''" ui:"hide:list" doc:"ISO 4217 code this subscription is billed in" required:"false" readOnly:"true"`

	// TrialUsedAt is when this tenant's trial was issued, and it is never
	// cleared. A subscription that cancelled and resubscribed used to get a
	// fresh trial, so four cancellations were four free periods; now the first
	// one is the only one, and a resubscribe starts active with a charge due at
	// the end of the period it has just been given.
	TrialUsedAt *time.Time `json:"trialUsedAt,omitempty" gorm:"type:timestamptz" ui:"hide:list" doc:"When this tenant's one trial was issued" readOnly:"true"`

	// AttemptCount and PastDueSince are the dunning state. A charge that does
	// not settle increments the count and starts the clock; a charge that does
	// clears both. Past GraceDays the subscription is cancelled, which is the
	// ceiling the old module had and this one had lost: without it a dead card
	// was retried every night forever.
	AttemptCount int        `json:"attemptCount" gorm:"not null;default:0" ui:"hide:list" doc:"Consecutive charges that did not settle" required:"false" readOnly:"true"`
	PastDueSince *time.Time `json:"pastDueSince,omitempty" gorm:"type:timestamptz" ui:"hide:list" doc:"When this subscription first failed to pay" readOnly:"true"`

	// AnchorDay is the day of the month this subscription bills on, and it is
	// stored rather than derived from the period being served because a period
	// that had to be clamped must not become the anniversary. See Advance.
	AnchorDay int `json:"anchorDay,omitempty" gorm:"not null;default:0" ui:"hide:list" doc:"Day of the month this subscription bills on" required:"false" readOnly:"true"`
}

// GraceDays is how long a past-due subscription is still served. Seven days is
// what the module this replaces used, and it is the number a card that expired
// on the first of the month needs: the customer is written to, they update it,
// and the next night's renewal settles.
const GraceDays = 7

// CancelledByDunning is the reason on the billing.cancelled a grace period
// running out publishes. It is a constant because a subscriber that treats a
// customer who left differently from one who did not pay reads this field, and
// a string spelled twice is a subscriber that silently stops matching.
const CancelledByDunning = "dunning"

// TableName pins the table, so the entity and migrations/000016 agree.
func (Subscription) TableName() string { return "billing_subscriptions" }

// Expired reports whether the period being served has run out.
func (s *Subscription) Expired(now time.Time) bool { return !now.Before(s.CurrentPeriodEnd) }

// Validate is the entity's own check.
func (s *Subscription) Validate(context.Context) error {
	if s.Status == "" {
		s.Status = StatusTrial
	}
	s.Currency = strings.ToUpper(strings.TrimSpace(s.Currency))
	switch {
	case s.PlanID == uuid.Nil:
		return fmt.Errorf("a subscription is to a plan")
	case !slices.Contains(statuses, s.Status):
		return fmt.Errorf("status %q is not a lifecycle state", s.Status)
	case !s.CurrentPeriodEnd.After(s.CurrentPeriodStart):
		return fmt.Errorf("a period ends after it starts")
	case s.PriceCents < 0 || s.PriceCents > MaxPriceCents:
		return fmt.Errorf("a stamped price of %d is not one this module will bill", s.PriceCents)
	// A free subscription carries no currency, because there is nothing to
	// denominate; anything with a price carries a real one.
	case s.PriceCents > 0 && !ValidCurrency(s.Currency):
		return fmt.Errorf("currency %q is not an ISO 4217 code", s.Currency)
	case s.AttemptCount < 0:
		return fmt.Errorf("a number of failed attempts is not negative")
	case s.AnchorDay < 0 || s.AnchorDay > 31:
		return fmt.Errorf("%d is not a day of the month to bill on", s.AnchorDay)
	}
	return nil
}

// GraceExpired reports whether a past-due subscription has been past due longer
// than the grace period allows.
func (s *Subscription) GraceExpired(now time.Time) bool {
	return s.PastDueSince != nil && now.Sub(*s.PastDueSince) >= GraceDays*24*time.Hour
}

// Outstanding is the end of the period this subscription still owes for, and
// false when it owes for none.
//
// It is PastDueSince that answers and not Status, and that is the whole of the
// dunning fix: a subscription cancelled while it owed money still owes, so
// cancelling and resubscribing is not a way of resetting the attempt count and
// the grace clock. A period that is paid for clears both, in advance.
func (s *Subscription) Outstanding() (time.Time, bool) {
	if s.PastDueSince == nil {
		return time.Time{}, false
	}
	return s.CurrentPeriodEnd, true
}

// Charge is one attempt to take money for one period. It carries the plan's
// code and the period as well as the amount, so a provider's own record can be
// reconciled against this one.
type Charge struct {
	// Subject is what is being paid for. Here it is the subscription's id; the
	// field is named for the role rather than for this module's entity because
	// the private catalogue's payment module fills it with the payment's own
	// id, and a field called Subscription holding a payment id is a field that
	// lies in every log line it appears in. It was Subscription until E5.fix.
	Subject     uuid.UUID `json:"subjectId"`
	PlanCode    string    `json:"planCode"`
	AmountCents int64     `json:"amountCents"`
	Currency    string    `json:"currency"`
	PeriodEnd   time.Time `json:"periodEnd"`

	// IdempotencyKey is what the provider must key its own record on, so that
	// this charge attempted twice — a job that ran again after a crash, a
	// retry after a timeout whose answer was lost — takes the money once.
	//
	// It is derived and not generated: the subject and the period being paid
	// for are the whole identity of a charge, so the same period asked for
	// twice is the same key. A provider that ignores it will double-charge a
	// customer the first time a worker is killed between the call and the
	// commit, which is why this is stated as a requirement of the interface
	// below rather than as a hint.
	IdempotencyKey string `json:"idempotencyKey"`
}

// Key is the idempotency key of a charge for subject's period ending at end. It
// is a function so that a provider reconciling its own records builds the same
// string this module does.
func Key(subject uuid.UUID, end time.Time) string {
	return subject.String() + ":" + end.UTC().Format(time.RFC3339)
}

// Receipt is what a provider says about a charge.
//
// There are three answers and not two. Settled is money taken. Pending is a
// provider that has accepted the charge and cannot yet say whether it worked —
// a card payment awaiting a bank, a transfer, a processor that settles
// overnight — and it is neither paid nor unpaid: the subscription is left
// exactly as it was and asked about again tomorrow. Neither flag is the third
// answer, a refusal, and that is what makes a subscription past due.
//
// Pending exists because without it a provider that settles later had to lie in
// one direction or the other, and both lies are expensive: false was a customer
// marked past due for a payment that went through, and true was a period served
// for a payment that never did.
type Receipt struct {
	// Reference is the provider's own identifier, kept so a payment can be
	// found in somebody else's system.
	Reference string    `json:"reference"`
	Settled   bool      `json:"settled"`
	Pending   bool      `json:"pending,omitempty"`
	At        time.Time `json:"at"`
}

// PaymentProvider takes money. There is one implementation here, Manual, which
// records what is owed and moves nothing; the ones that speak to a payment
// processor live outside this repository, because a reference architecture
// carrying a Stripe client would be teaching Stripe.
//
// It takes no transaction, and that is the shape of the whole renewal below: a
// charge is a call to somebody else's machine.
//
// An implementation must honour Charge.IdempotencyKey: the same key twice is
// one payment. Every processor worth using has a header for it, and this
// module will present the same key for the same period for as long as that
// period goes unpaid.
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
	//
	// A plan change is refused while the subscription is past due, with
	// ErrConflict. That is the review's escape route closed: a tenant that owed
	// money moved itself to a cheaper plan and the debt went with the old
	// price. What is owed is owed for a period already served, and it is
	// settled or the grace period runs out; it is not renegotiated.
	//
	// The trial is issued once per tenant, ever, and TrialUsedAt is what says
	// so: it is read across every row this tenant has, deleted ones included,
	// rather than inferred from there being no live subscription. A first
	// subscription is served before any money is asked for; a resubscribe is
	// not, because four cancellations used to be four free periods.
	//
	// A resubscribe carries the outstanding period, the attempt count and the
	// grace clock forward untouched. A tenant that owed money when it left owes
	// it when it comes back, and comes back past due.
	Subscribe(ctx context.Context, tx db.Tx[db.Tenant], planID uuid.UUID) (*Subscription, error)

	// Cancel ends the subscription: at the end of the period it is serving, or
	// now. Cancelling twice the same way changes nothing; ending it now after
	// asking to end later is a second decision and says so; asking to end later
	// after it has ended is a conflict.
	//
	// Ending it now is refused with ErrConflict while a period is outstanding,
	// naming that period. That is the dunning ceiling's other escape route
	// closed: cancelling now and resubscribing took the subscription out of
	// past_due, and a review did it live for as many free periods as it liked.
	// A customer who owes money may still leave at the end of the period.
	Cancel(ctx context.Context, tx db.Tx[db.Tenant], atPeriodEnd bool) (*Subscription, error)

	// Renew advances a subscription whose period has run out, and it is the
	// half of a renewal where no money moves: a period still running is left
	// alone, a subscription whose customer cancelled is ended — silently,
	// because Cancel already said when it would — and a free subscription
	// starts its next period and publishes billing.renewed. A past-due one
	// whose grace period has run out is cancelled, with
	// billing.cancelled{reason: dunning}, which is the ceiling on retrying a
	// dead card. Otherwise it returns the charge and changes nothing; the
	// caller takes that money outside every transaction and brings the answer
	// to Settle.
	//
	// The charge is for the price stamped on the subscription and not the
	// plan's current one, so a price list edit applies from the next period.
	Renew(ctx context.Context, tx db.Tx[db.Tenant]) (*Subscription, *Charge, error)

	// Settle records what a provider said. A settled receipt starts the next
	// period and publishes billing.renewed; a refused one leaves the period
	// alone, marks the subscription past due and publishes billing.past_due —
	// once, because the job runs every night and a customer whose card is dead
	// must not be an event a night forever. A pending receipt is neither: the
	// subscription is left exactly as it was, nothing is published, and the
	// same charge is presented again tomorrow under the same idempotency key.
	// A charge for a period the subscription has moved past is ignored and says
	// nothing.
	Settle(ctx context.Context, tx db.Tx[db.Tenant], c Charge, r Receipt) (*Subscription, error)
}
