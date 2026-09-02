// Package contracts is everything another module, an app or a test may know
// about the audit trail: the row, the query, the permission and the Service
// interface. The implementation is in ../internal.
//
// This module subscribes to every event every other module declares, so it
// records what already happened rather than keeping a second account of it.
// That is why there is no action taxonomy here, no entity type and no
// before/after: what happened is the event's name and the payload its module
// already published, and a schema that normalised those is a schema every new
// module has to be taught. A module is audited by having emitted an event.
//
// It is append-only. Nothing updates a row and nothing removes one but the
// retention job, which is also why there is no rest.Spec: a Spec is five routes
// and three of them write.
package contracts

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
)

// Event is one thing that happened in one tenant, as the trail recorded it.
//
// It is not a crud.Entity and embeds no crud.Base: there is no updated_at
// because nothing updates it, and no deleted_at because a soft delete is a way
// of changing history quietly. TenantID comes from the transaction the row was
// written in and never from the envelope, and is json:"-" for the reason
// crud.Base's is. OccurredAt is when the state changed — when the outbox row
// was written — and not when the worker got round to it. Actor is nil when
// nobody caused it: a job, an event handler, the bootstrap. EventID is the
// outbox event's own id, which is what makes recording idempotent whatever
// redelivers it. The columns are migrations/000010.
type Event struct {
	ID         uuid.UUID       `json:"id" format:"uuid" doc:"The trail row's own id"`
	TenantID   uuid.UUID       `json:"-"`
	OccurredAt time.Time       `json:"occurredAt" doc:"When the state changed"`
	Name       string          `json:"name" doc:"The event's name" example:"task.task.created"`
	Actor      *uuid.UUID      `json:"actor,omitempty" format:"uuid" doc:"The user who caused it, absent for system work"`
	EventID    uuid.UUID       `json:"eventId" format:"uuid" doc:"The event this row records"`
	Payload    json.RawMessage `json:"payload" doc:"The event's payload, as its module published it"`
}

// TableName pins the table, so the struct and migrations/000010 agree.
func (Event) TableName() string { return "audit_events" }

// Query is a page of the trail: what happened, who did it, and when. It is a
// struct of its own rather than a crud.Query because two of the filters are
// range comparisons and crud's are equalities. An empty Name is every kind of
// event and the nil Actor is everybody; Since is inclusive and Until exclusive,
// so two adjacent windows neither overlap nor skip. The zero value is
// everything, newest first.
type Query struct {
	Name          string
	Actor         uuid.UUID
	Since, Until  time.Time
	Limit, Offset int
}

// Service is the audit trail: one command and two reads.
//
// Record takes the caller's transaction rather than opening one, and the reason
// is sharper here than elsewhere: it is called from an event handler, and the
// row has to commit with the claim that says the event was handled. A trail
// written in a transaction of its own would record events whose handling then
// rolled back. The errors are kit/crud's.
type Service interface {
	// Record writes the trail row for one event. Recording the same event again
	// changes nothing and, like everything here, publishes nothing: an audit of
	// audits is a loop.
	Record(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error

	// List is a page of this tenant's trail, newest first, with the total the
	// page came from. Get is one row of it.
	List(ctx context.Context, tx db.Tx[db.Tenant], q Query) ([]*Event, int64, error)
	Get(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*Event, error)
}
