package internal_test

// The six operation columns answer one question, asked from the operation side: which events
// did this call cause. Writing them is one INSERT whose column names are a string and whose
// values a positional slice (service.go); reading them is a WHERE on operation_ref and the
// column-to-field mapping into the six fields of contracts.Event, which is all a consumer of
// the trail ever sees. The columns are pinned at this module's root, the two read steps by
// nothing: a List that ignored OperationRef, or answered rows whose operation fields are NULL,
// leaves every name, count and refusal here reading correctly while the operation-side question
// gets the tenant's whole trail, or a page of silent absences.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	kitoperation "github.com/septagon-oss/platformkit/kit/operation"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

// TestTheTrailAnswersForTheEventsOneOperationCaused asks the trail the operation-side question
// through its own List. One operation causes several events, so the answer is a set: four rows
// are recorded in one tenant — two from one operation, one from another, one from none — and
// what the first caused must come back as those two rows, each still naming the operation's id,
// ref, tenant, person, outcome and revision, with nothing answered for an occurrence no event
// carried. Every assertion reads a row count or a field of the contract, so the case passes on
// any code that keeps the filter and the mapping.
func TestTheTrailAnswersForTheEventsOneOperationCaused(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()

	// Each fact carries its own value, so a fact read off the wrong column is a wrong answer
	// rather than a coincidence. The payload names no id, so a row is found by its operation
	// and not by the records a payload mentions.
	cause := kitoperation.Identity{ID: "op-asked", Ref: uuid.NewString(), Tenant: uuid.NewString(),
		Person: uuid.NewString(), Outcome: kitoperation.Succeeded, Revision: 7}
	unasked := kitoperation.Identity{ID: "op-unasked", Ref: uuid.NewString(), Tenant: uuid.NewString(),
		Person: uuid.NewString(), Outcome: kitoperation.Failed, Revision: 2}
	at := db.Now()
	for _, ev := range [4]events.Event{
		trailEvent("task.task.created", cause, at.Add(-time.Minute)),
		trailEvent("task.task.completed", cause, at),
		trailEvent("task.task.completed", unasked, at),
		trailEvent("audit.test_recorded", kitoperation.Identity{}, at),
	} {
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return svc.Record(ctx, tx, ev)
		})
		if err != nil {
			t.Fatalf("record %s: %v", ev.Name, err)
		}
	}

	rows, total := askTrail(t, conn, acme, svc, contracts.Query{OperationRef: uuid.MustParse(cause.Ref)})
	if total != 2 || len(rows) != 2 {
		t.Fatalf("asked what %s caused, the trail answers %d row(s) over %v, want the two it wrote",
			cause.ID, total, namesOf(rows))
	}
	wants := [...]struct{ fact, want string }{
		{"operation id", cause.ID}, {"operation ref", cause.Ref},
		{"operation tenant", cause.Tenant}, {"operation person", cause.Person},
		{"operation outcome", string(cause.Outcome)}, {"operation revision", fmt.Sprint(cause.Revision)},
	}
	for _, row := range rows {
		answers := [...]string{shown(row.OperationID), shown(row.OperationRef), shown(row.OperationTenantID),
			shown(row.OperationPersonID), shown(row.OperationOutcome), shown(row.OperationRevision)}
		for i, w := range wants {
			if answers[i] != w.want {
				t.Errorf("the row for %s answers %s %q, not the %q the event carried", row.Name, w.fact, answers[i], w.want)
			}
		}
	}

	// An occurrence that caused nothing is answered with nothing: a filter that matched
	// everything would pass the two rows above and fail here.
	if _, total := askTrail(t, conn, acme, svc, contracts.Query{OperationRef: uuid.New()}); total != 0 {
		t.Errorf("asked for an operation nothing carried, the trail answers %d rows, want none", total)
	}
}

// trailEvent is one event of one operation, with an id of its own.
func trailEvent(name string, op kitoperation.Identity, at time.Time) events.Event {
	return events.Event{Name: name, ID: uuid.New(), At: at.UTC(), Operation: op,
		Payload: json.RawMessage(`{"title":"chiller-2"}`)}
}

// askTrail reads one tenant's trail the way its own read door does.
func askTrail(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, svc *internal.Service,
	q contracts.Query) ([]*contracts.Event, int64) {
	t.Helper()
	var rows []*contracts.Event
	var total int64
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		rows, total, err = svc.List(ctx, tx, q)
		return err
	})
	if err != nil {
		t.Fatalf("ask the trail for operation %s: %v", q.OperationRef, err)
	}
	return rows, total
}

// shown is the field as the answer hands it: NULL for the absence, the value for the fact.
func shown[T any](v *T) string {
	if v == nil {
		return "NULL"
	}
	return fmt.Sprint(*v)
}
