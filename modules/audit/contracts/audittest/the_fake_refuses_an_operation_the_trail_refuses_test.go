package audittest_test

// Record is the half of contracts.Service that decides what the trail never
// holds: an operation nobody can attribute is refused before a row exists. The
// shared suite records no operation at all, and the composed handler's refusals
// are answered by the service behind it, so this holds the same rule for the
// double every module composes against. The double matters because it copies the
// cause's ids out of the envelope as UUIDs: with the check that refuses them gone,
// an operation named by an id alone is a crash rather than the refusal the trail
// answers with, and a module testing its own attribution against the double would
// never hear about it.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	kitoperation "github.com/septagon-oss/platformkit/kit/operation"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/audit/contracts/audittest"
)

// TestTheFakeRefusesAnOperationTheTrailRefuses records each operation the kernel
// cannot attribute and requires the double to answer with the kernel's own reason
// and to keep no row: one named by an id with no ref, tenant or revision, one
// whose outcome answers nothing, one whose ref is not a UUID and one whose person
// is not. An operation the kernel can attribute is then recorded, so the case is
// not satisfied by a double that refuses every event nor by one that stores the
// refused ones.
func TestTheFakeRefusesAnOperationTheTrailRefuses(t *testing.T) {
	f := audittest.NewFake()
	ref, tenant := uuid.NewString(), uuid.NewString()
	for _, c := range []struct {
		what, fault string
		op          kitoperation.Identity
	}{
		{"an operation named by an id alone", "invalid operation metadata", kitoperation.Identity{ID: "op-nothing"}},
		{"an operation whose outcome answers nothing", "invalid operation metadata",
			kitoperation.Identity{ID: "op-undecided", Outcome: "undecided", Revision: 1}},
		{"an operation whose ref is not a UUID", "operationref",
			kitoperation.Identity{ID: "op-bad-ref", Ref: "not-a-uuid", Tenant: tenant, Revision: 1}},
		{"an operation whose person is not a UUID", "operationpersonid",
			kitoperation.Identity{ID: "op-bad-person", Ref: ref, Tenant: tenant, Person: "not-a-uuid", Revision: 1}},
	} {
		err := refused(func() error {
			return f.Record(t.Context(), db.Tx[db.Tenant]{}, events.Event{
				Name: "audit.test_recorded", ID: uuid.New(), Payload: json.RawMessage(`{}`), Operation: c.op,
			})
		})
		switch {
		case err == nil:
			t.Errorf("the fake accepted %s, an operation the kernel cannot attribute", c.what)
		case !strings.Contains(err.Error(), c.fault):
			t.Errorf("the fake refused %s with %q, which names no fault of the operation", c.what, err)
		}
	}
	if _, total, err := f.List(t.Context(), db.Tx[db.Tenant]{}, contracts.Query{}); err != nil || total != 0 {
		t.Errorf("after four refusals the fake holds %d row(s) (%v), want none", total, err)
	}

	granted := kitoperation.Identity{ID: "op-granted", Ref: ref, Tenant: tenant, Revision: 1}
	if err := f.Record(t.Context(), db.Tx[db.Tenant]{}, events.Event{
		Name: "audit.test_recorded", ID: uuid.New(), Payload: json.RawMessage(`{}`), Operation: granted,
	}); err != nil {
		t.Fatalf("the fake refused an operation it can attribute: %v", err)
	}
	rows, total, err := f.List(t.Context(), db.Tx[db.Tenant]{}, contracts.Query{OperationRef: uuid.MustParse(ref)})
	if err != nil || total != 1 || len(rows) != 1 || rows[0].OperationID == nil || *rows[0].OperationID != granted.ID {
		t.Errorf("the fake answers %d row(s) of operation %v (%v) for an attributed event, want its one row", total, ref, err)
	}
}

// refused is what Record answers, with a panic reported as the refusal it should
// have been and never as a pass. The double reads the cause's ref, tenant and
// person as UUIDs, so an operation naming them badly crashes the moment the check
// in front of it is gone; a case that died with the process would name no rule.
func refused(record func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("the fake panicked instead of refusing: %v", p)
		}
	}()
	return record()
}
