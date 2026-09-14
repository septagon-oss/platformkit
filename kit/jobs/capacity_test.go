package jobs

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
)

// BenchmarkPerTenantCapacity writes four rows in four transactions per tenant,
// behind the same advisory lock a nonparallel scheduled job holds. Every pass
// lists 64 tenants through a committed system transaction, then performs 256
// RLS-protected updates. There is no injected sleep, HTTP traffic or broker.
func BenchmarkPerTenantCapacity(b *testing.B) {
	for _, poolSize := range []int{2, 5, 16} {
		for _, workers := range []int{1, 4} {
			b.Run(fmt.Sprintf("pool=%d/workers=%d", poolSize, workers), func(b *testing.B) {
				adminURL, appURL := dbtest.URLsFor(b)
				if err := db.Migrate(b.Context(), adminURL, migrations.Source); err != nil {
					b.Fatal(err)
				}
				admin := dbtest.OpenFor(b, adminURL)
				pool := db.DefaultPool()
				pool.MaxOpenConns, pool.MaxIdleConns = poolSize, min(4, poolSize)
				conn, err := db.OpenWithPool(b.Context(), appURL, pool)
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { _ = conn.Close() })
				if _, err := admin.ExecContext(b.Context(), `CREATE TABLE capacity_tenants (id uuid PRIMARY KEY, slug text);
                    CREATE TABLE capacity_rows (tenant_id uuid NOT NULL, row_number integer, visits integer NOT NULL DEFAULT 0, PRIMARY KEY (tenant_id, row_number));
                    ALTER TABLE capacity_rows ENABLE ROW LEVEL SECURITY;
                    ALTER TABLE capacity_rows FORCE ROW LEVEL SECURITY;
                    CREATE POLICY capacity_tenant ON capacity_rows USING (tenant_id = platformkit_current_tenant_id());`); err != nil {
					b.Fatal(err)
				}
				const tenants, rows = 64, 4
				for i := range tenants {
					id := uuid.New()
					if _, err := admin.ExecContext(b.Context(), "INSERT INTO capacity_tenants VALUES ($1, $2)", id, fmt.Sprint(i)); err != nil {
						b.Fatal(err)
					}
					if _, err := admin.ExecContext(b.Context(), "INSERT INTO capacity_rows (tenant_id, row_number) SELECT $1, generate_series(0, $2)", id, rows-1); err != nil {
						b.Fatal(err)
					}
				}
				listing := listFunc(func(ctx context.Context, tx db.Tx[db.System]) ([]tenancy.Tenant, error) {
					var result []tenancy.Tenant
					err := tx.DB().Raw("SELECT id, slug FROM capacity_tenants ORDER BY slug").Scan(&result).Error
					return result, err
				})
				var active, peak atomic.Int64
				work := func(ctx context.Context, conn *db.Conn, tenant tenancy.Tenant) error {
					running := active.Add(1)
					defer active.Add(-1)
					for old := peak.Load(); running > old && !peak.CompareAndSwap(old, running); old = peak.Load() {
					}
					for row := range rows {
						if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
							result := tx.DB().Exec("UPDATE capacity_rows SET visits = visits + 1 WHERE row_number = ?", row)
							if result.Error != nil {
								return result.Error
							}
							if result.RowsAffected != 1 {
								return fmt.Errorf("tenant %s updated %d rows", tenant.Slug, result.RowsAffected)
							}
							return nil
						}); err != nil {
							return err
						}
					}
					return nil
				}
				name := unique("capacity")
				before := conn.Stats()
				for b.Loop() {
					unlock, ok, err := db.TryLock(b.Context(), conn, name)
					if err != nil || !ok {
						b.Fatalf("job lock = %v, %v", ok, err)
					}
					err = PerTenantConcurrent(b.Context(), conn, listing, workers, work)
					unlock()
					if err != nil {
						b.Fatal(err)
					}
				}
				after := conn.Stats()
				var visits int64
				if err := admin.QueryRowContext(b.Context(), "SELECT sum(visits) FROM capacity_rows").Scan(&visits); err != nil {
					b.Fatal(err)
				}
				if want := int64(b.N) * tenants * rows; visits != want {
					b.Fatalf("committed updates = %d, want %d", visits, want)
				}
				if peak.Load() > int64(workers) || after.InUse != 0 || after.OpenConnections > poolSize {
					b.Fatalf("work escaped bounds: peak=%d pool=%+v", peak.Load(), after)
				}
				b.ReportMetric(float64(b.N*tenants)/b.Elapsed().Seconds(), "tenant/s")
				b.ReportMetric(float64(after.WaitCount-before.WaitCount)/float64(b.N), "pool-waits/pass")
				b.ReportMetric(float64(after.WaitDuration-before.WaitDuration)/float64(b.N*tenants), "pool-wait-ns/tenant")
				b.ReportMetric(float64(peak.Load()), "max-callbacks")
			})
		}
	}
}
