package idempotency_test

import (
	"context"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestConcurrentClaimsHaveOneOwner(t *testing.T) {
	store, ctx := inPostgres(t)
	s := scope("concurrent-owner")
	type result struct {
		verdict idempotency.Verdict
		err     error
	}
	results := make(chan result, 2)
	begin := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			<-begin
			v, err := store.Claim(ctx, s, start)
			results <- result{v, err}
		})
	}
	close(begin)
	wg.Wait()
	close(results)
	owners, waiting := 0, 0
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		switch r.verdict.Kind {
		case idempotency.First:
			owners++
		case idempotency.InFlight:
			waiting++
		default:
			t.Errorf("unexpected verdict = %v", r.verdict.Kind)
		}
	}
	if owners != 1 || waiting != 1 {
		t.Errorf("owners=%d waiting=%d, want one each", owners, waiting)
	}
}

func TestAnotherTenantCannotReadOrChangeAnIdempotencyRecord(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	store := idempotency.Postgres(httpx.ConnFrom)
	s := scope("tenant-isolation")
	if _, err := store.Claim(httpx.WithConn(t.Context(), conn), s, start); err != nil {
		t.Fatal(err)
	}
	ctx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: other, Slug: "other"})
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var count int64
		if err := tx.DB().Table("platformkit_idempotency").Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("another tenant read %d records", count)
		}
		result := tx.DB().Exec("UPDATE platformkit_idempotency SET status = 599 WHERE tenant = ?", s.Tenant)
		if result.RowsAffected != 0 {
			t.Errorf("another tenant changed %d records", result.RowsAffected)
		}
		return result.Error
	}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := admin.QueryRow("SELECT count(*) FROM platformkit_idempotency WHERE tenant = $1 AND status IS NULL", s.Tenant).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("owner's unchanged records = %d, want 1", count)
	}
}
