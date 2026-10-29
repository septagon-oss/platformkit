package audittest_test

// The double promises to mirror internal.Service.Record, and the service checks
// the operation before it touches the table. Order is the promise here: an event
// whose row is already stored, replayed with a cause the kernel cannot attribute,
// must still come back refused. A double that answered "already have it" would
// tell a consumer its broken cause was accepted, and the same test run against a
// Postgres would answer the opposite. Nothing else in this package reaches that
// pair: the shared suite replays only well-formed events, and the refusal cases
// record each event once.

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

// TestTheDoubleRefusesAReplayedEventWhoseCauseCannotBeAttributed stores one event
// by an operation the kernel can attribute, then replays that same event id with
// a ref that is not a UUID. The replay must be refused with the kernel's own
// reason and must leave the stored row intact and readable, so the double refuses
// a cause it cannot attribute whether or not it has already seen the event.
func TestTheDoubleRefusesAReplayedEventWhoseCauseCannotBeAttributed(t *testing.T) {
	f := audittest.NewFake()
	ref, tenant := uuid.NewString(), uuid.NewString()
	granted := kitoperation.Identity{ID: "op-replayed", Ref: ref, Tenant: tenant, Revision: 3}
	at := db.Now()
	eventID := uuid.New()
	record := func(op kitoperation.Identity) (err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("the double panicked instead of refusing: %v", p)
			}
		}()
		return f.Record(t.Context(), db.Tx[db.Tenant]{}, events.Event{
			Name: "audit.test_recorded", ID: eventID, At: at,
			Payload: json.RawMessage(`{"title":"chiller-2"}`), Operation: op,
		})
	}

	if err := record(granted); err != nil {
		t.Fatalf("the double refused an operation it can attribute: %v", err)
	}
	broken := kitoperation.Identity{ID: "op-replayed", Ref: "not-a-uuid", Tenant: tenant, Revision: 3}
	err := record(broken)
	switch {
	case err == nil:
		t.Error("the double accepted a replayed event whose cause it cannot attribute, an operation the kernel refuses")
	case !strings.Contains(err.Error(), "operationref"):
		t.Errorf("the double refused the replayed event with %q, which names no fault of the operation", err)
	}

	rows, total, err := f.List(t.Context(), db.Tx[db.Tenant]{}, contracts.Query{OperationRef: uuid.MustParse(ref)})
	if err != nil || total != 1 || len(rows) != 1 || rows[0].OperationID == nil || *rows[0].OperationID != granted.ID {
		t.Errorf("after the refusal the trail answers %d row(s) of operation %v (%v), want the one row it stored", total, ref, err)
	}
}
