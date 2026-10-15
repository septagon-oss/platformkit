package events_test

// A declared member nothing can project is refused, not left unconstrained.
//
// schema.go states the rule for a member it cannot project — "Nil means 'no
// constraint' … an honest unknown is checked by nobody rather than wrongly by
// everybody" — and jsonSchema() honours it by emitting JSON Schema's `true`.
// check() does not: it recurses into the nil sub-schema (`p.Schema.check(…)`
// and `s.Items.check(…)`) and dereferences it. So a declared payload type with
// one member the projection cannot describe — a json.RawMessage, an `any`, a
// []any, a map with non-string keys, or a member that marshals itself — makes
// the *check* panic rather than skip, inside the publisher's authoritative
// transaction (kit/db/tx.go rolls back and re-panics; kit/httpx answers 500).
//
// The brief's acceptance is "a mis-shaped payload is refused": refused, with a
// reason naming the event and the JSON path. A panic is not a refusal, gives
// the publisher no reason, and is not what the file beside it promises. The
// reference composition has no such member today, so nothing here is red until
// the next module declares one — which is the shape of a kernel defect.
//
// The correct behaviour is the one the comment already states: an unprojectable
// member constrains nothing. Fix check() to treat a nil sub-schema the way
// Validate and jsonSchema already do and every assertion below passes.

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// unprojectableIssued is a payload a module author is entitled to write: three
// members the projection says nothing about, exactly as schema.go's own comment
// describes them.
type unprojectableIssued struct {
	Raw  json.RawMessage `json:"raw"`
	Any  any             `json:"any"`
	List []any           `json:"list"`
}

const unprojectableBody = `{"raw":{"a":1},"any":"text","list":[1,"two"]}`

func declareManifest(t *testing.T, list ...events.Declared) {
	t.Helper()
	events.DeclareAll(list)
	t.Cleanup(func() { events.DeclareAll(nil) })
}

// publishManifest returns the error and, separately, whatever the call panicked
// with, so a panic is a reported result rather than a dead test binary.
func publishManifest(t *testing.T, conn *db.Conn, name string, payload any) (err error, panicked any) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			panicked = r
		}
	}()
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, name, payload)
	})
	return err, nil
}

// TestAMemberTheProjectionCannotDescribeConstrainsNothing is the rule at the
// mechanism: the projection of json.RawMessage, any and []any is nil, so those
// three members accept whatever arrives.
func TestAMemberTheProjectionCannotDescribeConstrainsNothing(t *testing.T) {
	s := events.Declare[unprojectableIssued]("billing.invoice_issued").Schema()
	if s == nil {
		t.Fatal("the payload type itself projected to nothing; the case below needs the object")
	}
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Schema.Validate panicked on %s, which its own package says it does not check: %v", unprojectableBody, r)
			}
		}()
		err = s.Validate([]byte(unprojectableBody))
	}()
	if err != nil {
		t.Fatalf("the three members the projection describes as unknown were refused: %v", err)
	}
	// And the projection still says what it says, so the document an integrator
	// reads is not the thing that broke.
	doc, jsonErr := json.Marshal(s)
	if jsonErr != nil {
		t.Fatal(jsonErr)
	}
	for _, member := range []string{"raw", "any", "list"} {
		if !strings.Contains(string(doc), `"`+member+`"`) {
			t.Errorf("the schema lost %s: %s", member, doc)
		}
	}
	if got := events.SchemaOf(reflect.TypeFor[[]any]()); got != nil && got.Type != "array" {
		t.Errorf("[]any projected to %+v", got)
	}
}

// TestTheDoorDoesNotPanicOnAMemberItCannotCheck is the same rule at the door the
// brief names: the outbox either takes the row or refuses it with a reason.
func TestTheDoorDoesNotPanicOnAMemberItCannotCheck(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	declareManifest(t, events.Declare[unprojectableIssued]("billing.invoice_issued"))

	err, panicked := publishManifest(t, conn, "billing.invoice_issued", json.RawMessage(unprojectableBody))
	if panicked != nil {
		t.Fatalf("Publish panicked instead of answering: %v", panicked)
	}
	if err != nil {
		t.Fatalf("the outbox refused a payload whose every member the projection calls unknown: %v", err)
	}
	var n int
	if qErr := admin.QueryRow(`SELECT count(*) FROM platformkit_outbox WHERE name = 'billing.invoice_issued'`).Scan(&n); qErr != nil {
		t.Fatal(qErr)
	}
	if n != 1 {
		t.Errorf("the accepted publish wrote %d rows, want 1", n)
	}

	// The check still bites where it can see: the same door refuses a payload
	// that is not an object at all, naming the event and the path, and writes
	// nothing (rule 9). This is the half that must survive the fix.
	err, panicked = publishManifest(t, conn, "billing.invoice_issued", "a string, not the payload")
	if panicked != nil {
		t.Fatalf("Publish panicked instead of refusing: %v", panicked)
	}
	if err == nil {
		t.Fatal("the outbox accepted a payload that is not an object")
	}
	if !strings.Contains(err.Error(), "billing.invoice_issued") || !strings.Contains(err.Error(), "$") {
		t.Errorf("the refusal names neither the event nor the path: %v", err)
	}
	if qErr := admin.QueryRow(`SELECT count(*) FROM platformkit_outbox WHERE name = 'billing.invoice_issued'`).Scan(&n); qErr != nil {
		t.Fatal(qErr)
	}
	if n != 1 {
		t.Errorf("the refused publish wrote %d rows, want the one from the accepted call", n)
	}
	// The tenant still owns its own row; nothing here reached another tenant.
	if uuid.Nil == acme.ID {
		t.Error("the test tenant lost its identifier")
	}
}
