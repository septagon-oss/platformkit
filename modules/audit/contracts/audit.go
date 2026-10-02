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
// redelivers it. The columns are migrations/000010 and, for the four that name a
// cause which was no session, migrations/000033.
type Event struct {
	ID         uuid.UUID       `json:"id" format:"uuid" doc:"The trail row's own id"`
	TenantID   uuid.UUID       `json:"-"`
	OccurredAt time.Time       `json:"occurredAt" doc:"When the state changed"`
	Name       string          `json:"name" doc:"The event's name" example:"task.task.created"`
	Actor      *uuid.UUID      `json:"actor,omitempty" format:"uuid" doc:"The user who caused it, absent for system work"`
	EventID    uuid.UUID       `json:"eventId" format:"uuid" doc:"The event this row records"`
	Payload    json.RawMessage `json:"payload" doc:"The event's payload, as its module published it"`

	// Attribution: what caused this when the cause was not a session. The outbox
	// carries these four (kit/events.Attribution, migrations/000032) and this trail
	// is where they outlive it, because the relay deletes a published row once its
	// retention window passes and the trail is append-only: a copy that dropped
	// them is the last chance the installation had to say what caused the write.
	// ActorKind names the sort of cause and is the only one of the four that may
	// stand alone — the seed run that named no file is still a seed run. Actor and
	// ActorKind are both absent for a job with nobody behind it, and Actor is absent
	// while ActorKind is 'seed' for a run that served a person: `actor` is who was
	// signed in, and no session wrote this. Initiator is that person, and is not
	// Actor — the trail's Actor is a login, and no login wrote a seed run's row.
	// SourceFile and SourceLine are cited together or not at all: the pair rule is
	// kit/events.Attribution's, and Record keeps it here because this table holds no
	// constraint of its own on the pair. migrations/000033 says why nothing here is
	// backfilled.
	ActorKind  *string    `json:"actorKind,omitempty" doc:"What kind of cause wrote this, when it was no session: user, system, seed or job" example:"seed"`
	SourceFile *string    `json:"sourceFile,omitempty" maxLength:"512" doc:"The file that asked for the write, cited by the run that made it" example:"seed/starter/contents.yaml"`
	SourceLine *int       `json:"sourceLine,omitempty" doc:"The line of that file" example:"12"`
	Initiator  *uuid.UUID `json:"initiator,omitempty" format:"uuid" doc:"The person the run served, who did not sign in"`
}

// TableName pins the table, so the struct and the migrations that made it —
// 000010 and 000033 — agree.
func (Event) TableName() string { return "audit_events" }

// Query is a page of the trail: what happened, who did it, when, and to what.
// It is a struct of its own rather than a crud.Query because two of the filters
// are range comparisons and crud's are equalities. An empty Name is every kind
// of event and the nil Actor is everybody; Since is inclusive and Until
// exclusive, so two adjacent windows neither overlap nor skip. The zero value
// is everything, newest first.
//
// Record is the trail of one row, and it is a search of the payload rather than
// a column because a payload names its row differently depending on who
// published it: a generated write carries the row, so the id is "id", while a
// command carries its own argument, where it is "taskId" or "contentId". An
// event is about a row when the row's id appears anywhere in its payload, which
// is true of both shapes and of any module that follows either.
type Query struct {
	Name          string
	Actor         uuid.UUID
	Record        uuid.UUID
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
