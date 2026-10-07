package contracts

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
)

// The two closed sets the delivery log speaks. They are spelled here as well as
// in the column CHECK constraints of migrations/000045: a tag is what the schema
// and the generated screen read, this is what the Go code compares against, and
// nothing can derive one from the other.
const (
	// StateQueued means a row exists that nobody has opened a socket for yet.
	StateQueued = "queued"
	// StateInFlight means a socket was opened and no outcome has been written.
	// It is the state a crash leaves behind, which is why a claim also takes an
	// in-flight row whose lease has expired rather than trusting it forever.
	StateInFlight = "in_flight"
	// StateSent is a 2xx. Terminal.
	StateSent = "sent"
	// StateFailed is an attempt that may be answered again: a transport error,
	// a timeout, a 5xx, a 429. Terminal for *this* row; the retry is a new row.
	StateFailed = "failed"
	// StateDropped is an attempt that will never be answered again: a client
	// error the receiver will keep answering, a closed window, an address this
	// deployment will not dial, an endpoint that was deleted. Terminal.
	StateDropped = "dropped"
)

var states = []string{StateQueued, StateInFlight, StateSent, StateFailed, StateDropped}

// Terminal reports whether a state is one nothing will ever move again.
func Terminal(state string) bool {
	return state == StateSent || state == StateFailed || state == StateDropped
}

// The three reasons a delivery row exists, which is how an administrator tells
// the brief's three apart: a first attempt, a retry the ladder scheduled, and a
// delivery a person asked for by hand.
const (
	CauseInitial = "initial"
	CauseRetry   = "retry"
	CauseReplay  = "replay"
)

var causes = []string{CauseInitial, CauseRetry, CauseReplay}

// Payload is the CloudEvent as published — the exact bytes POSTed to the
// receiver and the exact bytes the signature is computed over. It is written
// once, at fan-out, and never rewritten, which is what makes a retry sign the
// same string its first attempt did.
type Payload json.RawMessage

// Value stores the document as jsonb. The cast happens in the statement that
// inserts it, which is the only way to keep jsonb from reformatting the bytes
// this module has to sign byte-for-byte — see the note on the column.
func (p Payload) Value() (driver.Value, error) {
	if len(p) == 0 {
		return nil, nil
	}
	return string(p), nil
}

// Scan reads the document back, and refuses to guess: a payload that is not
// JSON is a broken row, not an empty one.
func (p *Payload) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*p = nil
	case []byte:
		*p = append((*p)[:0], v...)
	case string:
		*p = append((*p)[:0], v...)
	default:
		return fmt.Errorf("webhook: a delivery payload is JSON and this is %T", src)
	}
	return nil
}

// MarshalJSON answers with the document itself, so the log's row renders as the
// event it carries rather than as an array of numbers.
func (p Payload) MarshalJSON() ([]byte, error) {
	if len(p) == 0 {
		return []byte("null"), nil
	}
	if !json.Valid(p) {
		return nil, fmt.Errorf("webhook: delivery payload is not JSON")
	}
	return json.RawMessage(p).MarshalJSON()
}

// UnmarshalJSON reads the document back for the client.
func (p *Payload) UnmarshalJSON(b []byte) error {
	*p = append((*p)[:0], b...)
	return nil
}

// Delivery is one attempt to deliver one event to one endpoint: the row the
// administrator reads when they ask "did it go out, what did they answer, and
// did anybody retry it".
//
// One row per attempt, never one row per event. A retry is a fact about a
// different moment — a different socket, a different answer, a different
// timestamp on the signature — and a log that overwrote its own rows would
// answer "was there a 500" with "no", because the last attempt succeeded.
//
// This is not a second copy of notification_deliveries. That table answers
// "was this person told", over channels whose subject is a person; this one
// answers "did this event leave for that URL, and what did they answer", over
// rows whose subject is a machine address a tenant registered. One event can
// produce rows in both, and neither is a projection of the other.
type Delivery struct {
	crud.Base

	// Seq is the order the rows were appended in, which clock_timestamp() alone
	// cannot give: every row one transaction writes would share a timestamp.
	Seq int64 `json:"seq" gorm:"->;column:seq" readOnly:"true" format:"int64" doc:"The log's own order"`

	EndpointID uuid.UUID `json:"endpointId" gorm:"type:uuid;not null" readOnly:"true" doc:"The endpoint this attempt was addressed to" format:"uuid"`
	// EventID is the CloudEvent id, which is also the webhook-id header, which
	// is what a receiver deduplicates on.
	EventID   uuid.UUID `json:"eventId" gorm:"type:uuid;not null" readOnly:"true" doc:"The event's own id, sent as webhook-id" format:"uuid"`
	EventName string    `json:"eventName" gorm:"type:text;not null" readOnly:"true" doc:"The event type that matched, so the log answers why this delivery exists"`
	// Payload is the event document as published: the bytes signed and POSTed.
	Payload Payload `json:"payload" gorm:"column:payload;type:jsonb" readOnly:"true" ui:"hide:list" doc:"The CloudEvent as published, and the exact bytes signed"`

	Attempt int    `json:"attempt" gorm:"not null;default:1" readOnly:"true" doc:"1 for the first attempt; the retry ladder's ordinal"`
	Cause   string `json:"cause" gorm:"type:text;not null" enum:"initial,retry,replay" ui:"widget:select" readOnly:"true" doc:"Why this row exists: a first attempt, a retry, or a replay an administrator asked for"`
	State   string `json:"state" gorm:"type:text;not null" enum:"queued,in_flight,sent,failed,dropped" ui:"widget:select" readOnly:"true" doc:"What became of this attempt"`

	// AvailableAt is the retry ladder: when a queued row may be sent. For a
	// first attempt it is the moment the event arrived.
	AvailableAt time.Time `json:"availableAt" gorm:"type:timestamptz;not null" readOnly:"true" ui:"widget:datetime" doc:"When this attempt may be made"`
	// AttemptedAt is when the socket was opened; nil while queued.
	AttemptedAt *time.Time `json:"attemptedAt,omitempty" gorm:"type:timestamptz" readOnly:"true" ui:"widget:datetime;hide:list" doc:"When the request was sent"`
	// StatusCode is the HTTP answer, 0 when nothing answered — no connection,
	// no TLS handshake, no lease honoured.
	StatusCode int `json:"statusCode" gorm:"not null;default:0" readOnly:"true" doc:"The HTTP status the receiver answered, 0 when nothing answered"`
	// Reason is the refusal or the failure in words an administrator reads.
	Reason string `json:"reason,omitempty" gorm:"type:text;not null;default:''" ui:"hide:list" readOnly:"true" doc:"Why this attempt failed, was dropped, or was retried"`
}

// TableName pins the table, so the entity and migrations/000045 agree.
func (Delivery) TableName() string { return "webhook_deliveries" }

// Validate is the log's own check. The application writes these rows and no
// caller does — the Spec offers List and Read only — so this is a guard against
// a defect in this module, which is exactly what a write-side check is for.
func (d *Delivery) Validate(context.Context) error {
	if !slicesContains(states, d.State) {
		return fmt.Errorf("state %q is not a delivery state", d.State)
	}
	if !slicesContains(causes, d.Cause) {
		return fmt.Errorf("cause %q is not a delivery cause", d.Cause)
	}
	if d.Attempt < 1 {
		return fmt.Errorf("an attempt is numbered from 1, not %d", d.Attempt)
	}
	if len(d.Payload) == 0 {
		return fmt.Errorf("a delivery without a payload would sign nothing")
	}
	if !ValidEventName(d.EventName) {
		return fmt.Errorf("%q is not an event name", d.EventName)
	}
	return nil
}

// slicesContains is slices.Contains with the one-line explanation this file
// would otherwise need twice.
func slicesContains(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}
