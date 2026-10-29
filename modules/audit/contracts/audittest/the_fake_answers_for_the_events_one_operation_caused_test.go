package audittest_test

// The conformance suite records no operation at all — `grep -rn "Operation"
// modules/audit/contracts/audittest/conformance*.go` prints nothing — so the
// fake's own half of the operation-side read is held by no case: neither the
// OperationRef arm of its filter nor the six facts Record copies off the event.
// A module that tests against this fake rather than a Postgres is reading the
// trail through these lines, so a filter that stopped honouring OperationRef
// would answer "what did this call cause" with the tenant's whole trail while
// every suite in the repository stayed green. The real service's half of the
// same question is pinned in modules/audit/internal; this is the fake's half,
// asked the way a consumer of contracts.Service asks it.

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	kitoperation "github.com/septagon-oss/platformkit/kit/operation"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/audit/contracts/audittest"
)

// TestTheFakeAnswersForTheEventsOneOperationCaused asks the fake which events one
// operation caused. Four are recorded — two from one operation, one from another,
// one from none, each fact of each cause carrying its own value — and the answer
// must be the two rows of the asked-for operation, still naming its id, ref,
// tenant, person, outcome and revision, with nothing answered for an occurrence
// nothing carried. Every assertion reads a row count or a field of
// contracts.Event, so the case passes on any fake that filters and copies.
func TestTheFakeAnswersForTheEventsOneOperationCaused(t *testing.T) {
	f := audittest.Fixture{Ctx: t.Context(), Service: audittest.NewFake(),
		Published: func() []string { return nil }}
	asked := kitoperation.Identity{ID: "op-asked", Ref: uuid.NewString(), Tenant: uuid.NewString(),
		Person: uuid.NewString(), Outcome: kitoperation.Succeeded, Revision: 7}
	unasked := kitoperation.Identity{ID: "op-unasked", Ref: uuid.NewString(), Tenant: uuid.NewString(),
		Person: uuid.NewString(), Outcome: kitoperation.Failed, Revision: 2}
	at := db.Now()
	for _, ev := range []events.Event{
		oneCause("task.task.created", asked, at.Add(-time.Minute)),
		oneCause("task.task.completed", asked, at),
		oneCause("task.task.completed", unasked, at),
		oneCause("audit.test_recorded", kitoperation.Identity{}, at),
	} {
		if err := f.Service.Record(f.Ctx, f.Tx, ev); err != nil {
			t.Fatalf("record %s: %v", ev.Name, err)
		}
	}

	rows, total, err := f.Service.List(f.Ctx, f.Tx, contracts.Query{OperationRef: uuid.MustParse(asked.Ref)})
	if err != nil {
		t.Fatalf("ask the fake what %s caused: %v", asked.ID, err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("asked what %s caused, the fake answers %d row(s), want the two it wrote", asked.ID, total)
	}
	facts := [...]struct{ fact, want string }{
		{"operation id", asked.ID}, {"operation ref", asked.Ref},
		{"operation tenant", asked.Tenant}, {"operation person", asked.Person},
		{"operation outcome", string(asked.Outcome)}, {"operation revision", fmt.Sprint(asked.Revision)},
	}
	for _, row := range rows {
		answers := [...]string{shown(row.OperationID), shown(row.OperationRef), shown(row.OperationTenantID),
			shown(row.OperationPersonID), shown(row.OperationOutcome), shown(row.OperationRevision)}
		for i, w := range facts {
			if answers[i] != w.want {
				t.Errorf("the row for %s answers %s %q, not the %q the event carried", row.Name, w.fact, answers[i], w.want)
			}
		}
	}

	// An occurrence that caused nothing is answered with nothing: a filter that
	// matched everything would pass the two rows above and fail here.
	if _, total, err := f.Service.List(f.Ctx, f.Tx, contracts.Query{OperationRef: uuid.New()}); err != nil || total != 0 {
		t.Errorf("asked for an operation nothing carried, the fake answers %d rows (%v), want none", total, err)
	}
}

// oneCause is one event of one operation, with an id of its own, so a row is
// found by its operation and not by what its payload mentions.
func oneCause(name string, op kitoperation.Identity, at time.Time) events.Event {
	return events.Event{Name: name, ID: uuid.New(), At: at.UTC(), Operation: op,
		Payload: json.RawMessage(`{"title":"chiller-2"}`)}
}

// shown is the field as the fake hands it: NULL for the absence, the value for the fact.
func shown[T any](v *T) string {
	if v == nil {
		return "NULL"
	}
	return fmt.Sprint(*v)
}
