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
// It is append-only in the sense the code makes true: nothing in this module
// updates a row and nothing removes one but the retention job, which is also why
// there is no rest.Spec — a Spec is five routes and three of them write.
//
// Say plainly what that does not include. The trail carries the request that caused
// each row — actor, request id, client address, trace context (migrations/000035) —
// and the database refuses a rewrite of it: migrations/000048 revokes UPDATE and
// TRUNCATE from every grantee the catalog discovers, and installs a BEFORE UPDATE
// trigger that refuses every role, the table's owner included, beside a BEFORE DELETE
// trigger that admits only a role that may delete and may not insert, and only past
// 365 days. What it is still not is hash-chained: the per-tenant sequence and
// prev_hash/hash pair decision 0013 owes is undelivered, so what this proves is that
// nothing inside the application's reach rewrote a row, not that no row was ever
// inserted. modules/audit/README.md's Duties section carries the same account with the
// command that checks it.
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
	// RequestID is the id the caller of the request that caused this was answered
	// with (X-Request-ID), and TraceParent the W3C trace context it carried, kept
	// verbatim as the standard spells it. ClientIP is the peer address of the
	// connection the request arrived on — never a header a client could write.
	// All three are absent for work no request caused, and for every row written
	// before migrations/000035: a fact nobody captured cannot be reconstructed
	// afterwards, and inventing one would be writing history twice.
	RequestID string `json:"requestId,omitempty" doc:"The call that caused this, as its caller was told it" example:"0f7c0f1c-2a3e-4a1b-9b4f-2f1d0c9b8a71"`
	ClientIP  string `json:"clientIp,omitempty" doc:"The address the call arrived from" example:"203.0.113.7"`
	// Traceparent is spelled as the standard spells its header, in one word, and
	// that is not a style choice: the runner maps a scanned field by snake_case,
	// so a Go name of TraceParent reads a column named trace_parent and finds
	// nothing — the failure kit/events/relay.go warns about, reached here first.
	Traceparent string `json:"traceparent,omitempty" doc:"The W3C trace context of the call that caused this" example:"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"`
	// TraceID is the trace this row was written in, as the uuid a trace backend
	// and a query can answer from: the second field of Traceparent, read out of it
	// on the way back rather than stored beside it. The trail keeps one string —
	// two columns for one fact are two columns that can disagree, and migrations/
	// 000037 indexes the id out of the one column that exists — and this field is
	// what makes that join answerable without the caller parsing a header. It is
	// absent, not zero, when the row carries no traceparent or an unparseable one:
	// "no trace" and "the trace whose id is all zeros" are different facts.
	TraceID *uuid.UUID `json:"traceId,omitempty" format:"uuid" gorm:"-" doc:"The trace this event happened in, absent when nobody traced it"`
}

// TableName pins the table, so the struct and migrations/000010 agree.
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
//
// Request and TraceID are the two questions the row could not answer until
// migrations/000035: which call wrote this, and which trace that call belonged
// to. The first matches the stored id exactly; the second matches the trace id,
// which is the second field of the stored traceparent and what
// migrations/000037 indexes.
type Query struct {
	Name          string
	Actor         uuid.UUID
	Record        uuid.UUID
	Request       string
	TraceID       string
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
