package events_test

// The outbox refuses a payload that is not what the emitting module declared.
// The refusal is the point of the catalog, so it is tested at the door every
// event passes through rather than at the schema checker beside it.

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// invoiceIssued is the test module's payload. Its tags are the contract: what
// encoding/json writes from it and what the schema says are one projection
// (kit/events/schema.go), so a case below that disagrees with the type is a bug
// in the type and not in the test.
type invoiceIssued struct {
	InvoiceID uuid.UUID `json:"invoiceId"`
	Total     int64     `json:"total"`
	Currency  string    `json:"currency"`
	Due       time.Time `json:"due"`
	Note      string    `json:"note,omitempty"`
	Lines     []struct {
		SKU      string `json:"sku"`
		Quantity int    `json:"quantity"`
	} `json:"lines"`
	Meta map[string]string `json:"meta,omitempty"`
}

// declare installs the test composition's catalog for one name. A real
// composition gets this from kit/app.New; a test that publishes a declared name
// has to say what it declared.
func declare(t *testing.T, list ...events.Declared) {
	t.Helper()
	events.DeclareAll(list)
	t.Cleanup(func() { events.DeclareAll(nil) })
}

func publishErr(t *testing.T, conn *db.Conn, name string, payload any) error {
	t.Helper()
	return db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, name, payload)
	})
}

func TestAMisShapedPayloadIsRefusedAtTheOutbox(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	declare(t, events.Declare[invoiceIssued]("billing.invoice_issued"))

	good := invoiceIssued{
		InvoiceID: uuid.New(), Total: 4200, Currency: "EUR",
		Due: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		Lines: []struct {
			SKU      string `json:"sku"`
			Quantity int    `json:"quantity"`
		}{{SKU: "KIT-1", Quantity: 2}},
		Meta: map[string]string{"order": "O-7"},
	}
	if err := publishErr(t, conn, "billing.invoice_issued", good); err != nil {
		t.Fatalf("the payload the type itself produces was refused: %v", err)
	}

	for _, tc := range []struct {
		name, want string
		payload    any
	}{
		{"a bare string is not the payload", "is a string, want an object", "not an object"},
		{"no body at all", "null", nil},
		{"money is a number", "$.total", map[string]any{
			"invoiceId": uuid.NewString(), "total": "42.00", "currency": "EUR",
			"due": "2026-10-01T00:00:00Z", "lines": []any{}}},
		{"the identifier is missing", "$.invoiceId", map[string]any{
			"total": 1, "currency": "EUR", "due": "2026-10-01T00:00:00Z", "lines": []any{}}},
		{"a timestamp that is not one", "RFC 3339", map[string]any{
			"invoiceId": uuid.NewString(), "total": 1, "currency": "EUR",
			"due": "first of next month", "lines": []any{}}},
		{"an identifier that is not one", "is not a UUID", map[string]any{
			"invoiceId": "INV-1", "total": 1, "currency": "EUR",
			"due": "2026-10-01T00:00:00Z", "lines": []any{}}},
		{"a map value that is not a string", "$.meta", map[string]any{
			"invoiceId": uuid.NewString(), "total": 1, "currency": "EUR",
			"due": "2026-10-01T00:00:00Z", "lines": []any{}, "meta": map[string]any{"n": 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := publishErr(t, conn, "billing.invoice_issued", tc.payload)
			if err == nil {
				t.Fatalf("the outbox accepted %v", tc.payload)
			}
			if !strings.Contains(err.Error(), "billing.invoice_issued") {
				t.Errorf("the error does not name the event: %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the error %q does not say %q", err, tc.want)
			}
			// A refused mutation writes nothing: not a row, and — because this
			// ran inside the caller's transaction — not the caller's change
			// either. Rule 9.
			var n int
			if err := admin.QueryRow(`SELECT count(*) FROM platformkit_outbox WHERE name = 'billing.invoice_issued'`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Errorf("the outbox holds %d rows after one accepted publish and %d refused ones", n, 1)
			}
		})
	}
}

// TestTheRefusalRollsBackTheTransactionThatAskedForIt: the publisher's own write
// is gone with it. A refused event that left its state change behind would be
// the exact failure the outbox exists to remove, seen from the other side.
func TestTheRefusalRollsBackTheTransactionThatAskedForIt(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	declare(t, events.Declare[invoiceIssued]("billing.invoice_issued"))

	other := tenancy.Tenant{ID: uuid.New(), Slug: "other"}
	err := db.Run(tenancy.WithTenant(t.Context(), other), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Exec("INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES (?, 'rolled-back', ?)",
			uuid.New(), db.TenantOf(tx).ID).Error; err != nil {
			return err
		}
		return events.Publish(ctx, tx, "billing.invoice_issued", "a string, not the payload")
	})
	if err == nil {
		t.Fatal("Publish accepted a payload its module never declared")
	}
	var n int
	if err := admin.QueryRow(`SELECT count(*) FROM platformkit_handled WHERE durable = 'rolled-back'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("the refused publish left %d of the transaction's rows behind", n)
	}
}

// TestAnUndeclaredNameIsNotChecked here is not an endorsement: kit/app refuses
// a composition whose route would publish a name no module declared, and that is
// the gate. What this pins is that the schema check adds no second error for
// the same mistake, and that a module emitting a payload the kernel cannot
// describe (Declared.Payload nil) is published rather than refused.
func TestAnUndeclaredNameIsNotChecked(t *testing.T) {
	_, conn := dbtest.Schema(t)
	declare(t, events.Declared{Name: "billing.plan_created"})
	if err := publishErr(t, conn, "billing.plan_created", map[string]any{"anything": true}); err != nil {
		t.Fatalf("an undeclared name was refused by the schema check: %v", err)
	}
	if err := publishErr(t, conn, "billing.other_name", "anything at all"); err != nil {
		t.Fatalf("a name with no declaration was refused: %v", err)
	}
}

// TestDeclareAllWithNothingLeavesTheCheckOff is the state a process that
// composed no modules is in: nothing promises anything, so nothing is refused.
func TestDeclareAllWithNothingLeavesTheCheckOff(t *testing.T) {
	_, conn := dbtest.Schema(t)
	events.DeclareAll(nil)
	if err := publishErr(t, conn, "billing.invoice_issued", "a string"); err != nil {
		t.Fatalf("a process with no catalog refused a publish: %v", err)
	}
}

// TestSchemaIsTheProjectionOfTheType, so an integrator reading the AsyncAPI
// document is reading the same description the outbox checks.
func TestSchemaIsTheProjectionOfTheType(t *testing.T) {
	doc, err := json.Marshal(events.Declare[invoiceIssued]("billing.invoice_issued").Schema())
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(doc, &s); err != nil {
		t.Fatal(err)
	}
	if s["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("no 2020-12 anchor: %v", s["$schema"])
	}
	if s["type"] != "object" {
		t.Fatalf("type = %v", s["type"])
	}
	props, _ := s["properties"].(map[string]any)
	for _, want := range []string{"invoiceId", "total", "currency", "due", "lines"} {
		if _, ok := props[want]; !ok {
			t.Errorf("properties carries no %s: %s", want, doc)
		}
	}
	// omitempty is the difference between a member that may be absent and one
	// that may not: Note is not required, InvoiceID is.
	if _, ok := props["note"]; !ok {
		t.Errorf("an omitempty field still appears in the schema, which is right; %s", doc)
	}
	required, _ := s["required"].([]any)
	want := []any{"invoiceId", "total", "currency", "due", "lines"}
	if !reflect.DeepEqual(sorted(required), sorted(want)) {
		t.Errorf("required = %v, want %v", required, want)
	}
	id, _ := props["invoiceId"].(map[string]any)
	if id["format"] != "uuid" {
		t.Errorf("invoiceId format = %v", id["format"])
	}
	due, _ := props["due"].(map[string]any)
	if due["format"] != "date-time" {
		t.Errorf("due format = %v", due["format"])
	}
	lines, _ := props["lines"].(map[string]any)
	if lines["type"] != "array" {
		t.Errorf("lines type = %v", lines["type"])
	}
	meta, _ := props["meta"].(map[string]any)
	additional, _ := meta["additionalProperties"].(map[string]any)
	if additional["type"] != "string" {
		t.Errorf("meta additionalProperties = %v", meta["additionalProperties"])
	}
}

func sorted(v []any) []string {
	out := make([]string, 0, len(v))
	for _, x := range v {
		out = append(out, x.(string))
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
