package internal

import (
	"context"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// The retention sweep's own schedule and batch. A day of lag on forgetting is free
// — a retention period is measured in months — and one DELETE over a busy tenant's
// quarter of mail is a lock held for as long as it takes, so the sweep is a batch
// per transaction.
const (
	mailRetentionCron  = "23 4 * * *"
	mailRetentionBatch = 2000
)

// MailRetention is the other half of the promise the message columns make: an intent
// keeps the subject and the bodies it owes, which is what lets a mail be sent after
// the commit that decided it, and what would fill a database if nothing forgot.
//
// Only finished rows go. A queued, in_flight or unknown row is never deleted,
// whatever its age: forgetting a mail that is still owed, or one whose fate nobody
// knows, is how a mail gets lost quietly. The cost of that rule is worth saying —
// an unresolved intent stays until a person acts on it, and producing one needs a
// crash inside the send window — and the sweep is not allowed to be the thing that
// hides it.
func MailRetention(tenants jobs.TenantLister, days int) jobs.Job {
	if days <= 0 {
		days = contracts.MailIntentRetentionDays
	}
	return jobs.Job{
		Name: "notification-mail-retention",
		Cron: mailRetentionCron,
		Run: func(ctx context.Context, conn *db.Conn) error {
			return jobs.PerTenant(ctx, conn, tenants, func(ctx context.Context, conn *db.Conn, t tenancy.Tenant) error {
				if err := trimMail(ctx, conn, days); err != nil {
					return fmt.Errorf("notification: trim the mail intent log of %s: %w", t.Slug, err)
				}
				return nil
			})
		},
	}
}

// trimMail deletes this tenant's finished intents, a batch per transaction, until
// fewer than a batch remain. The cutoff is computed by the database, so two replicas
// whose clocks have drifted forget the same rows.
func trimMail(ctx context.Context, conn *db.Conn, days int) error {
	for {
		var deleted int64
		err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			res := tx.DB().Exec("DELETE FROM "+intentTable+" WHERE id IN ("+
				"SELECT id FROM "+intentTable+
				" WHERE state IN (?, ?, ?) AND updated_at < now() - (? || ' days')::interval"+
				" ORDER BY updated_at, seq LIMIT ?)",
				contracts.MailIntentSent, contracts.MailIntentSuppressed, contracts.MailIntentFailed,
				fmt.Sprint(days), mailRetentionBatch)
			deleted = res.RowsAffected
			return res.Error
		})
		if err != nil || deleted < mailRetentionBatch {
			return err
		}
	}
}
