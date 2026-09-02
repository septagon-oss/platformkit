package internal

import (
	"context"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Hourly, because a retention period is measured in months and a day of lag on
// forgetting is free; a thousand rows per transaction, because one DELETE over
// a year of a busy tenant's trail is a lock held for as long as it takes.
const (
	retentionCron  = "0 * * * *"
	retentionBatch = 1000
)

// Retention is the module's periodic work: the trail forgets what it is no
// longer obliged to keep. It is a job and not a subscription because nothing
// happens when a row expires — the clock passes, which is the distinction
// docs/adr/0004 draws. It publishes nothing, like everything else here.
func Retention(tenants jobs.TenantLister, days int) jobs.Job {
	return jobs.Job{
		Name: "audit-retention",
		Cron: retentionCron,
		Run: func(ctx context.Context, conn *db.Conn) error {
			return jobs.PerTenant(ctx, conn, tenants, func(ctx context.Context, conn *db.Conn, t tenancy.Tenant) error {
				if err := trim(ctx, conn, days); err != nil {
					return fmt.Errorf("audit: trim the trail of %s: %w", t.Slug, err)
				}
				return nil
			})
		},
	}
}

// trim deletes this tenant's expired rows, a batch per transaction, until fewer
// than a batch remain.
//
// jobs.PerTenant hands over a context carrying the tenant and no open
// transaction, so every db.Run below opens its own: a tenant with a million
// expired rows is a thousand short transactions rather than one long one, and a
// worker asked to stop half way has already committed what it deleted. The
// cutoff is computed by the database, so two workers whose clocks have drifted
// delete the same rows.
func trim(ctx context.Context, conn *db.Conn, days int) error {
	age := fmt.Sprintf("%d days", days)
	for {
		var deleted int64
		err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			res := tx.DB().Exec("DELETE FROM "+table+" WHERE id IN ("+
				"SELECT id FROM "+table+" WHERE occurred_at < now() - ?::interval"+
				" ORDER BY occurred_at LIMIT ?)", age, retentionBatch)
			deleted = res.RowsAffected
			return res.Error
		})
		if err != nil || deleted < retentionBatch {
			return err
		}
	}
}
