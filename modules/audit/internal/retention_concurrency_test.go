package internal_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
	"github.com/septagon-oss/platformkit/modules/audit"
)

// Hold real deletes at a table lock to observe the actual composed job, not a
// replacement callback. The job also holds its normal separate advisory lock.
func TestComposedRetentionBoundsWorkersAndPreservesOtherTenants(t *testing.T) {
	for _, poolSize := range []int{2, 5, 16} {
		t.Run(fmt.Sprint(poolSize), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			adminURL, appURL := dbtest.URLs(t)
			if err := db.Migrate(ctx, adminURL, migrations.Source); err != nil {
				t.Fatal(err)
			}
			admin := dbtest.Open(t, adminURL)
			pool := db.DefaultPool()
			pool.MaxOpenConns, pool.MaxIdleConns = poolSize, min(4, poolSize)
			conn, err := db.OpenWithPool(ctx, appURL, pool)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			tenants := make(lister, 8)
			for i := range tenants {
				tenants[i] = tenancy.Tenant{ID: uuid.New(), Slug: fmt.Sprint(i)}
			}
			unlisted := tenancy.Tenant{ID: uuid.New(), Slug: "unlisted"}
			for _, tenant := range append(slices.Clone(tenants), unlisted) {
				if _, err := admin.ExecContext(ctx, `INSERT INTO audit_events (tenant_id, occurred_at, name, event_id, payload)
                    VALUES ($1, now() - interval '40 days', 'expired', gen_random_uuid(), '{}'),
                           ($1, now(), 'kept', gen_random_uuid(), '{}')`, tenant.ID); err != nil {
					t.Fatal(err)
				}
			}
			job := audit.Module(audit.Deps{Tenants: tenants, RetentionDays: 30}).Jobs[0]
			if job.Name != "audit-retention" || job.Parallel {
				t.Fatal("retention lost its scheduled-job lock")
			}
			unlock, acquired, err := db.TryLock(ctx, conn, "retention-test:"+uuid.NewString())
			if err != nil || !acquired {
				t.Fatalf("advisory lock = %v, %v", acquired, err)
			}
			defer unlock()
			blocker, err := admin.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback()
			if _, err := blocker.ExecContext(ctx, "LOCK TABLE audit_events IN EXCLUSIVE MODE"); err != nil {
				t.Fatal(err)
			}
			before := conn.Stats()
			done := make(chan error, 1)
			var work sync.WaitGroup
			work.Go(func() { done <- job.Run(ctx, conn) })
			defer func() { cancel(); _ = blocker.Rollback(); work.Wait() }()
			want := 4
			if poolSize == 2 {
				want = 1
			}
			for {
				var waiting int
				if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity
                    WHERE application_name = current_setting('search_path')
                      AND wait_event_type = 'Lock' AND query LIKE 'DELETE FROM audit_events%'`).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting > want {
					t.Fatalf("%d blocked deletes exceed the %d-worker bound", waiting, want)
				}
				if waiting == want {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("retention ended before its deletes reached the lock: %v", err)
				case <-ctx.Done():
					t.Fatal("retention did not reach its worker bound")
				case <-time.After(10 * time.Millisecond):
				}
			}
			if stats := conn.Stats(); stats.InUse != want+1 || stats.WaitCount != before.WaitCount {
				t.Fatalf("retention exceeded the available pool or omitted its job lock: %+v", stats)
			}
			if err := blocker.Rollback(); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			for _, tenant := range append(slices.Clone(tenants), unlisted) {
				var old, kept int
				if err := admin.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE name='expired'), count(*) FILTER (WHERE name='kept')
                    FROM audit_events WHERE tenant_id=$1`, tenant.ID).Scan(&old, &kept); err != nil {
					t.Fatal(err)
				}
				wantOld := 0
				if tenant.ID == unlisted.ID {
					wantOld = 1
				}
				if old != wantOld || kept != 1 {
					t.Fatalf("tenant %s retained old=%d kept=%d, want %d/1", tenant.Slug, old, kept, wantOld)
				}
			}
		})
	}
}
