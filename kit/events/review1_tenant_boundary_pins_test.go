package events_test

// REVIEW round 1 (T-0109), the pinning test.
//
// Nothing here reports a defect. It pins the two assertions this branch is most
// easily able to lose silently, because both are facts of a column pair and a
// policy that the new migration did not touch:
//
//   1. the trace columns 000027 adds to platformkit_outbox are behind the same
//      row-level-security policy as the row they describe — a second tenant can
//      neither read them nor clear the first tenant's handling claim;
//   2. the tenant in the envelope is the transaction's tenant, so the address a
//      bridge subscribes to is the tenant that owns the data, and a document
//      whose subject names another tenant is refused rather than delivered.
//
// Both fail the moment anyone widens a policy, moves the relay into a tenant
// transaction, or lets a caller name the tenant of an envelope it was handed.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
)

func TestTheTraceColumnsAreTheirTenantsAndNoOnesElse(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	events.DeclareAll(nil)

	acmeTx := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	other := tenancy.Tenant{ID: uuid.New(), Slug: "other"}
	tr, ok := trace.FromRequestID(uuid.NewString())
	if !ok {
		t.Fatal("a request id does not seed a trace")
	}

	var eventID uuid.UUID
	if err := db.Run(trace.With(tenancy.WithActor(tenancy.WithTenant(t.Context(), acmeTx), uuid.New()), tr),
		conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := events.Publish(ctx, tx, "billing.invoice_issued", map[string]any{"n": 1}); err != nil {
				return err
			}
			var id string
			if err := tx.DB().Raw("SELECT id::text FROM platformkit_outbox WHERE name = 'billing.invoice_issued'").
				Scan(&id).Error; err != nil {
				return err
			}
			parsed, perr := uuid.Parse(id)
			if perr != nil {
				return perr
			}
			eventID = parsed
			return nil
		}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// The row exists, with the trace on it.
	var stored string
	if err := admin.QueryRow(`SELECT traceparent FROM platformkit_outbox WHERE id=$1`, eventID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != tr.Parent() {
		t.Fatalf("the row carries %q, want the request's %s", stored, tr.Parent())
	}

	// A claim of the first tenant's, so there is something the second tenant
	// might be able to take away.
	if err := db.Run(tenancy.WithTenant(t.Context(), acmeTx), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES (?, 'review1', ?)",
			eventID, db.TenantOf(tx).ID).Error
	}); err != nil {
		t.Fatalf("write acme's claim: %v", err)
	}

	// The second tenant sees none of it: not the row, not its trace, not the
	// claim. Read-only assertions only, so nothing below can be a transaction
	// that Postgres has already aborted.
	if err := db.Run(tenancy.WithTenant(t.Context(), other), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var rows, claims int
		if err := tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE traceparent IS NOT NULL").Scan(&rows).Error; err != nil {
			return err
		}
		if rows != 0 {
			t.Errorf("tenant other reads %d of tenant acme's traced outbox rows", rows)
		}
		if err := tx.DB().Raw("SELECT count(*) FROM platformkit_handled WHERE event_id = ?", eventID).Scan(&claims).Error; err != nil {
			return err
		}
		if claims != 0 {
			t.Errorf("tenant other can see %d of tenant acme's handling claims", claims)
		}
		return nil
	}); err != nil {
		t.Fatalf("the second tenant's transaction: %v", err)
	}

	// And its own transaction cannot clear acme's claim: either the statement is
	// refused or it affects nothing. Either answer is the refusal; what is not
	// allowed is that the claim disappears. Its own transaction, so a refusal
	// here cannot abort the assertions above.
	if err := db.Run(tenancy.WithTenant(t.Context(), other), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		res := tx.DB().Exec("DELETE FROM platformkit_handled WHERE event_id = ?", eventID)
		if res.Error != nil {
			t.Logf("clearing another tenant's claim was refused: %v", res.Error)
			return nil // a refused write is the answer; roll nothing forward
		}
		if res.RowsAffected != 0 {
			t.Errorf("tenant other cleared %d of tenant acme's handling claims", res.RowsAffected)
		}
		return nil
	}); err != nil {
		t.Logf("the second tenant's delete never reached the table: %v", err)
	}

	var still, stillClaims int
	if err := admin.QueryRow(`SELECT count(*) FROM platformkit_outbox WHERE id=$1`, eventID).Scan(&still); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(`SELECT count(*) FROM platformkit_handled WHERE event_id=$1 AND durable='review1'`, eventID).Scan(&stillClaims); err != nil {
		t.Fatal(err)
	}
	if still != 1 {
		t.Error("the second tenant's transaction removed the first tenant's row")
	}
	if stillClaims != 1 {
		t.Error("the second tenant's transaction took away the first tenant's handling claim")
	}
}

func TestTheEnvelopeCannotNameATenantOtherThanItsOwn(t *testing.T) {
	tenantID := uuid.New()
	body, err := json.Marshal(transport.Event{
		ID: uuid.New(), Name: "billing.invoice_issued", TenantID: tenantID,
		Payload: json.RawMessage(`{"n":1}`), At: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["tenantid"] != tenantID.String() || doc["subject"] != transport.Subject(tenantID, "billing.invoice_issued") {
		t.Fatalf("the envelope does not name its tenant: %s", body)
	}
	// A document whose subject was rewritten to another tenant's address is the
	// one case where a bridge could hand tenant A's payload to tenant B's
	// consumer, so it is refused at the door rather than noticed downstream.
	rewritten := strings.Replace(string(body), transport.Subject(tenantID, "billing.invoice_issued"),
		transport.Subject(uuid.New(), "billing.invoice_issued"), 1)
	var ev transport.Event
	if err := json.Unmarshal([]byte(rewritten), &ev); err == nil {
		t.Errorf("a document whose subject names %s decoded as tenant %s", ev.TenantID, tenantID)
	} else if !strings.Contains(err.Error(), "subject") {
		t.Errorf("the refusal does not say which attribute is wrong: %v", err)
	}
}
