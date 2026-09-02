package contracts

import (
	"time"

	"github.com/google/uuid"
)

// The seven events this module emits: kit/rest's three for the plan catalogue,
// and the subscription lifecycle's four. Both sets are in the manifest, and
// kit/app refuses to start if a route would publish one that is not.
//
// A subscriber names one of these constants rather than a string, so renaming
// an event is a compile error in every module that listens for it.
const (
	EventPlanCreated = "billing.plan.created"
	EventPlanUpdated = "billing.plan.updated"
	EventPlanDeleted = "billing.plan.deleted"

	EventSubscribed = "billing.subscribed"
	EventCancelled  = "billing.cancelled"
	EventRenewed    = "billing.renewed"
	EventPastDue    = "billing.past_due"
)

// Events is every event this module emits, for the manifest.
var Events = []string{
	EventPlanCreated, EventPlanUpdated, EventPlanDeleted,
	EventSubscribed, EventCancelled, EventRenewed, EventPastDue,
}

// Subscribed is the payload of EventSubscribed: the tenant is on a plan. It
// carries the plan's code as well as its id, because a subscriber that gates a
// feature reads the code and should not need a second query for it.
type Subscribed struct {
	SubscriptionID uuid.UUID `json:"subscriptionId"`
	PlanID         uuid.UUID `json:"planId"`
	PlanCode       string    `json:"planCode"`
	Status         string    `json:"status"`
	PeriodEnd      time.Time `json:"periodEnd"`
	At             time.Time `json:"at"`
}

// Cancelled is the payload of EventCancelled: the customer has left. EndsAt is
// when service stops — now, or the end of the paid period — so a subscriber
// knows when to stop serving without knowing which of the two it was.
type Cancelled struct {
	SubscriptionID uuid.UUID `json:"subscriptionId"`
	EndsAt         time.Time `json:"endsAt"`
	Immediate      bool      `json:"immediate"`
	At             time.Time `json:"at"`
}

// Renewed is the payload of EventRenewed: a period was paid for and the next
// has started. Receipt is the provider's identifier, so a payment can be found
// in somebody else's system from this row.
type Renewed struct {
	SubscriptionID uuid.UUID `json:"subscriptionId"`
	PeriodStart    time.Time `json:"periodStart"`
	PeriodEnd      time.Time `json:"periodEnd"`
	Receipt        string    `json:"receipt,omitempty"`
	At             time.Time `json:"at"`
}

// PastDue is the payload of EventPastDue: a charge did not settle. It carries
// the amount because dunning is decided from it and a subscriber should not
// need a second query to write to the customer; Since is the unpaid period's end.
type PastDue struct {
	SubscriptionID uuid.UUID `json:"subscriptionId"`
	AmountCents    int64     `json:"amountCents"`
	Currency       string    `json:"currency"`
	Since          time.Time `json:"since"`
	At             time.Time `json:"at"`
}
