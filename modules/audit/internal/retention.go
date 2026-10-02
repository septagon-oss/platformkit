package internal

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// retainPool is the expiry connection's pool: four at most, one per tenant
// callback plus the lister, held for the length of one hourly run and closed.
// It is a small pool because the work is a sequence of short deletes, and a
// retention sweep that needs sixteen connections is a sweep that should be
// asking why.
func retainPool() db.Pool {
	return db.Pool{MaxOpenConns: 4, MaxIdleConns: 4, ConnMaxLifetime: 30 * time.Minute}
}

// Retention is the module's periodic work: the trail forgets what it is no
// longer obliged to keep, and writes down what it forgot. It is a job and not a
// subscription because nothing happens when a row expires — the clock passes,
// which is the distinction docs/adr/0004 draws. It publishes nothing, like
// everything else here, and the record it leaves is a row in
// audit_retention_marks, not an event: an expiry event would be relayed into the
// table the relay is trimming. A period below the floor spelled in that migration
// is refused at boot by config.Validate, which is where the number is readable.
//
// The connection the scheduler hands a job is the application's, and this job
// does not use it, the way kit/jobs' backfill worker does not use it for the
// runner's two tables. The reason here is the trigger: 00041 fences the
// application role's DELETE away — a role that may append to the trail may never
// expire it — so the expiry runs as the one role the fence admits, the one named
// by database.retain_url. That job opens its own connection, for one run, and
// closes it, which is the exception kit/jobs/jobs.go's comment about doubling
// the pool answers: an hourly job holding four more connections for the length
// of a sweep is not a second pool a request path waits behind.
//
// kit/db's Open is what stops that DSN being a superuser's: it refuses a role
// that row-level security would not bind, which is the whole point here — a
// BYPASSRLS trim would see every tenant's rows inside the first tenant's
// transaction and delete them, and no test in this repository could see it,
// because every test's owner connection is that role.
func Retention(tenants jobs.TenantLister, days int, retainURL string) jobs.Job {
	return jobs.Job{
		Name: "audit-retention",
		Cron: retentionCron,
		Run: func(ctx context.Context, _ *db.Conn) error {
			if retainURL == "" {
				return errors.New("audit: the trail cannot expire: database.retain_url names no role to " +
					"expire it with; name one, or accept that nothing in audit_events is ever removed")
			}
			conn, err := db.OpenWithPool(ctx, retainURL, retainPool())
			if err != nil {
				return fmt.Errorf("audit: open the retention connection: %w", err)
			}
			defer func() { _ = conn.Close() }()
			// Each trim holds one connection at a time; the lister holds another.
			workers := max(1, min(4, conn.Stats().MaxOpenConnections-1))
			return jobs.PerTenantConcurrent(ctx, conn, tenants, workers, func(ctx context.Context, conn *db.Conn, t tenancy.Tenant) error {
				if err := trim(ctx, conn, days); err != nil {
					return fmt.Errorf("audit: trim the trail of %s: %w", t.Slug, err)
				}
				return nil
			})
		},
	}
}

// trim deletes this tenant's expired rows, a batch per transaction, until fewer
// than a batch remain, and writes one mark per batch.
//
// jobs.PerTenantConcurrent hands over a tenant context without an open
// transaction, so every db.Run below opens its own: a tenant with a million
// expired rows is a thousand short transactions rather than one long one, and a
// worker asked to stop half way has already committed what it deleted.
//
// Each mark commits in the transaction of the DELETE it describes, which is what
// makes the record more than a log line: at every instant the rows the trail
// holds plus the rows the marks account for is what the outbox published, and a
// reader never sees a removal with no record or a record with no removal. The
// cutoff comes from the database, once per transaction, and is the value the
// DELETE applied rather than a value computed again afterwards, so two workers
// whose clocks have drifted agree on the boundary and record the one they used.
//
// A batch that removed nothing writes no mark: a mark describes a removal, and a
// heartbeat that says "the sweep ran and had nothing to do" is a row per hour per
// tenant in a table whose whole job is to account for rows that went away.
func trim(ctx context.Context, conn *db.Conn, days int) error {
	age := fmt.Sprintf("%d days", days)
	for {
		var deleted int64
		err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			var cutoff time.Time
			if err := tx.DB().Raw("SELECT now() - ?::interval", age).Scan(&cutoff).Error; err != nil {
				return err
			}
			res := tx.DB().Exec("DELETE FROM "+table+" WHERE id IN ("+
				"SELECT id FROM "+table+" WHERE occurred_at < ?"+
				" ORDER BY occurred_at LIMIT ?)", cutoff, retentionBatch)
			deleted = res.RowsAffected
			if res.Error != nil || deleted == 0 {
				return res.Error
			}
			return tx.DB().Exec("INSERT INTO audit_retention_marks (tenant_id, cutoff, removed) VALUES (?, ?, ?)",
				db.TenantOf(tx).ID, cutoff, deleted).Error
		})
		if err != nil || deleted < retentionBatch {
			return err
		}
	}
}
