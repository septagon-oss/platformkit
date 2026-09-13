package events_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
)

func inspectionRecords(t *testing.T, owner *sql.DB) string {
	t.Helper()
	var state string
	err := owner.QueryRowContext(t.Context(), `SELECT jsonb_build_array(
		(SELECT jsonb_agg(r ORDER BY id) FROM platformkit_outbox r),
		(SELECT jsonb_agg(r ORDER BY event_id, durable) FROM platformkit_handled r),
		(SELECT jsonb_agg(r ORDER BY event_id, durable) FROM platformkit_dead_letters r))::text`).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestInspectionReturnsBoundedOrderedMetadataWithoutChangingDelivery(t *testing.T) {
	owner, conn := dbtest.Schema(t)
	first, second := uuid.New(), uuid.New()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for i, id := range ids {
		tenant := first
		if i == 1 {
			tenant = second
		}
		_, err := owner.ExecContext(t.Context(), `INSERT INTO platformkit_outbox(id, tenant_id, name, payload, created_at)
			VALUES ($1, $2, 'fixture.pending', '{"secret":"payload-private"}', $3);
		`, id, tenant, time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := owner.ExecContext(t.Context(), `INSERT INTO platformkit_dead_letters(event_id, durable, tenant_id, name, error, failed_at)
			VALUES ($1, 'fixture.consumer', $2, 'fixture.failed', 'password=error-private', $3)`,
			id, tenant, time.Date(2026, 1, 1+i, 1, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.ExecContext(t.Context(), `INSERT INTO platformkit_handled(event_id, durable, tenant_id)
			VALUES ($1, 'fixture.consumer', $2)`, id, tenant); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := owner.ExecContext(t.Context(), `INSERT INTO platformkit_outbox(tenant_id, name, payload, published_at)
		VALUES ($1, 'fixture.published', '{}', now())`, first); err != nil {
		t.Fatal(err)
	}
	before := inspectionRecords(t, owner)
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		for _, limit := range []int{1, 2, 3, 100} {
			report, err := events.InspectDelivery(ctx, tx, limit)
			if err != nil {
				return err
			}
			if len(report.Pending) != min(limit, 3) || len(report.Failures) != min(limit, 3) ||
				report.PendingMore != (limit < 3) || report.FailuresMore != (limit < 3) {
				t.Fatalf("limit %d: %+v", limit, report)
			}
			if report.Pending[0].EventID != ids[0] || report.Failures[0].EventID != ids[2] {
				t.Fatal("inspection changed oldest-pending/latest-failure order")
			}
			if limit >= 2 && (report.Pending[1].TenantID != second || report.Failures[1].TenantID != second) {
				t.Fatal("system inspection omitted another tenant")
			}
			encoded, err := json.Marshal(report)
			if err != nil || strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "published") {
				t.Fatalf("inspection exposed excluded data: %s / %v", encoded, err)
			}
		}
		for _, limit := range []int{-1, 0, 101} {
			if result, err := events.InspectDelivery(ctx, tx, limit); err == nil || !reflect.DeepEqual(result, events.DeliveryInspection{}) {
				t.Fatalf("invalid limit returned a partial result: %+v / %v", result, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if after := inspectionRecords(t, owner); after != before {
		t.Fatal("inspection changed durable records")
	}
}

func TestInspectionEmptyFailureAndCancellationNeverReturnPartialRecords(t *testing.T) {
	owner, conn := dbtest.Schema(t)
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		report, err := events.InspectDelivery(ctx, tx, 50)
		if err != nil || len(report.Pending) != 0 || len(report.Failures) != 0 || report.PendingMore || report.FailuresMore {
			t.Fatalf("empty inspection: %+v / %v", report, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lock, err := owner.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback() }()
	if _, err := lock.ExecContext(t.Context(), "LOCK TABLE platformkit_outbox IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		report, err := events.InspectDelivery(ctx, tx, 50)
		if !errors.Is(err, context.DeadlineExceeded) || !reflect.DeepEqual(report, events.DeliveryInspection{}) {
			t.Fatalf("blocked inspection: %+v / %v", report, err)
		}
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	// A failure in the second read must also discard the successful first read.
	if _, err := owner.ExecContext(t.Context(), `INSERT INTO platformkit_outbox(tenant_id, name, payload)
		VALUES ($1, 'fixture.pending', '{}');`, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ExecContext(t.Context(), "ALTER TABLE platformkit_dead_letters RENAME TO inspection_unavailable"); err != nil {
		t.Fatal(err)
	}
	_ = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		report, err := events.InspectDelivery(ctx, tx, 50)
		if err == nil || !reflect.DeepEqual(report, events.DeliveryInspection{}) {
			t.Fatalf("failed second read returned a partial inspection: %+v / %v", report, err)
		}
		return err
	})
}

func TestInspectionKeepsCallerOwnedChangesProvisional(t *testing.T) {
	owner, conn := dbtest.Schema(t)
	before := inspectionRecords(t, owner)
	rollback := errors.New("caller refused its transaction")
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if err := events.PublishFor(ctx, tx, uuid.New(), "fixture.provisional", struct{}{}); err != nil {
			return err
		}
		report, err := events.InspectDelivery(ctx, tx, 50)
		if err != nil || len(report.Pending) != 1 || report.Pending[0].Name != "fixture.provisional" {
			t.Fatalf("own transaction's intent is not visible: %+v / %v", report, err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) || inspectionRecords(t, owner) != before {
		t.Fatal("inspection committed its caller's transaction")
	}
	if _, err := events.InspectDelivery(t.Context(), db.Tx[db.System]{}, 50); err == nil {
		t.Fatal("inspection accepted an unopened transaction")
	}
}
