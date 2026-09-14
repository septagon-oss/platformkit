package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type listFunc func(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error)

func (f listFunc) List(ctx context.Context, tx db.Tx[db.System]) ([]tenancy.Tenant, error) {
	return f(ctx, tx)
}

func tenantList(n int) lister {
	result := make(lister, n)
	for i := range result {
		result[i] = tenancy.Tenant{ID: uuid.New(), Slug: fmt.Sprint(i)}
	}
	return result
}

func TestConcurrentTenantWorkIsBoundedCommittedAndIsolated(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), `CREATE TABLE listed (id integer);
      CREATE TABLE visits (tenant_id uuid NOT NULL DEFAULT platformkit_current_tenant_id());
      ALTER TABLE visits ENABLE ROW LEVEL SECURITY;
      ALTER TABLE visits FORCE ROW LEVEL SECURITY;
      CREATE POLICY tenant_visits ON visits USING (tenant_id = platformkit_current_tenant_id());`); err != nil {
		t.Fatal(err)
	}
	tenants := tenantList(6)
	listing := listFunc(func(ctx context.Context, tx db.Tx[db.System]) ([]tenancy.Tenant, error) {
		return tenants, tx.DB().Exec("INSERT INTO listed VALUES (1)").Error
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, len(tenants))
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	var active, peak atomic.Int64
	first, later := errors.New("first failure"), errors.New("later failure")
	done := make(chan error, 1)
	go func() {
		done <- PerTenantConcurrent(ctx, conn, listing, 2, func(ctx context.Context, conn *db.Conn, tenant tenancy.Tenant) error {
			running := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); running > old && !peak.CompareAndSwap(old, running); old = peak.Load() {
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			var listed int
			if err := admin.QueryRowContext(ctx, "SELECT count(*) FROM listed").Scan(&listed); err != nil {
				return err
			}
			if listed != 1 {
				return errors.New("listing transaction was not committed before tenant work")
			}
			if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				if err := tx.DB().Exec("INSERT INTO visits DEFAULT VALUES").Error; err != nil {
					return err
				}
				var own []uuid.UUID
				if err := tx.DB().Raw("SELECT tenant_id FROM visits").Scan(&own).Error; err != nil {
					return err
				}
				if len(own) != 1 || own[0] != tenant.ID {
					return fmt.Errorf("tenant %s saw %v", tenant.Slug, own)
				}
				return nil
			}); err != nil {
				return err
			}
			switch tenant.Slug {
			case "0":
				return first
			case "3":
				return later
			}
			return nil
		})
	}()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("two workers did not start")
		}
	}
	select {
	case <-started:
		t.Error("a third callback started while both workers were blocked")
	case <-time.After(25 * time.Millisecond):
	}
	unblock()
	err := <-done
	if !errors.Is(err, first) || !errors.Is(err, later) || !strings.HasPrefix(err.Error(), "tenant 0:") {
		t.Fatalf("ordered partial failures = %v", err)
	}
	if peak.Load() != 2 || active.Load() != 0 {
		t.Fatalf("active/peak = %d/%d", active.Load(), peak.Load())
	}
	var count int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM visits").Scan(&count); err != nil || count != len(tenants) {
		t.Fatalf("committed work = %d, %v", count, err)
	}
}

func TestTenantCancellationStopsDispatchAndWaitsForCallbacks(t *testing.T) {
	_, conn := dbtest.Schema(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, 10)
	var completed atomic.Int64
	done := make(chan error, 1)
	go func() {
		done <- PerTenantConcurrent(ctx, conn, tenantList(10), 2, func(ctx context.Context, _ *db.Conn, _ tenancy.Tenant) error {
			started <- struct{}{}
			<-ctx.Done()
			completed.Add(1)
			return ctx.Err()
		})
	}()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("two workers did not start")
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	if completed.Load() != 2 || len(started) != 0 {
		t.Fatalf("callbacks completed=%d, extra started=%d", completed.Load(), len(started))
	}
}

func TestTenantWorkRejectsCallerTransactionsAndInvalidInput(t *testing.T) {
	_, conn := dbtest.Schema(t)
	fn := func(context.Context, *db.Conn, tenancy.Tenant) error { t.Error("callback ran"); return nil }
	listing := listFunc(func(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) {
		t.Error("lister ran")
		return nil, nil
	})
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, _ db.Tx[db.System]) error {
		if err := PerTenantConcurrent(ctx, conn, listing, 2, fn); !errors.Is(err, db.ErrScopeMismatch) {
			t.Errorf("ambient system = %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lazy, pending, err := db.Lazy(tenancy.WithTenant(t.Context(), tenantList(1)[0]), conn, dbtest.SystemToken())
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close(false)
	if err := PerTenant(lazy, conn, listing, fn); !errors.Is(err, db.ErrScopeMismatch) {
		t.Errorf("lazy transaction = %v", err)
	}
	if err := PerTenantConcurrent(t.Context(), nil, listing, 0, fn); err == nil {
		t.Error("accepted zero workers")
	}
	if err := PerTenantConcurrent(t.Context(), nil, listing, 1, nil); err == nil {
		t.Error("accepted nil callback")
	}
	canceled, stop := context.WithCancel(t.Context())
	stop()
	if err := PerTenantConcurrent(canceled, nil, listing, 2, fn); !errors.Is(err, context.Canceled) {
		t.Errorf("pre-canceled = %v", err)
	}
}
