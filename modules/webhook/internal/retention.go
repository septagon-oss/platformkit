package internal

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The retention sweep's own schedule and batch. A day of lag on forgetting is
// free — a retention period is measured in months — and one DELETE over a busy
// tenant's month of attempts is a lock held for as long as it takes, so the
// sweep is a batch per transaction.
const (
	retentionCron  = "17 3 * * *"
	retentionBatch = 2000
)

// Retention is the other half of the promise the payload column makes: every
// delivery row keeps the whole CloudEvent it signed, which is what lets a
// receiver's verification be reproducible, and what would fill a database if
// nothing forgot. Terminal rows older than the window go; a queued or in_flight
// row never does, whatever its age, because forgetting an attempt that had not
// been made yet is how an event gets lost quietly.
func Retention(tenants jobs.TenantLister, days int) jobs.Job {
	return jobs.Job{
		Name: "webhook-retention",
		Cron: retentionCron,
		Run: func(ctx context.Context, conn *db.Conn) error {
			return jobs.PerTenant(ctx, conn, tenants, func(ctx context.Context, conn *db.Conn, t tenancy.Tenant) error {
				if err := trim(ctx, conn, days); err != nil {
					return fmt.Errorf("webhook: trim the delivery log of %s: %w", t.Slug, err)
				}
				return nil
			})
		},
	}
}

// trim deletes this tenant's expired attempts, a batch per transaction, until
// fewer than a batch remain. The cutoff is computed by the database, so two
// replicas whose clocks have drifted forget the same rows.
func trim(ctx context.Context, conn *db.Conn, days int) error {
	for {
		var deleted int64
		err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			res := tx.DB().Exec("DELETE FROM "+deliveryTable+" WHERE id IN ("+
				"SELECT id FROM "+deliveryTable+
				" WHERE state IN ('sent', 'failed', 'dropped') AND updated_at < now() - (? || ' days')::interval"+
				" ORDER BY updated_at LIMIT ?)", fmt.Sprint(days), retentionBatch)
			deleted = res.RowsAffected
			return res.Error
		})
		if err != nil || deleted < retentionBatch {
			return err
		}
	}
}

// idInput is the one path parameter the hand-mounted routes share.
type idInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The row's id"`
}
