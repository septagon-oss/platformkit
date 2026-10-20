package idempotency_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestCommandEffectPurgeKeepsMarkersInsideRetention(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	store := idempotency.Postgres(httpx.ConnFrom)
	now := time.Now().UTC()
	for key, age := range map[string]time.Duration{"expired-effect": 25 * time.Hour, "retained-effect": 23 * time.Hour} {
		s := scope(key)
		if err := db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: s.Tenant, Slug: "acme"}), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return store.Mark(ctx, tx, s, uuid.New())
			}); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.ExecContext(t.Context(), `UPDATE platformkit_command_effect SET applied_at = $1 WHERE "key" = $2`, now.Add(-age), key); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := idempotency.Purge(t.Context(), conn); err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]int{"expired-effect": 0, "retained-effect": 1} {
			var count int
			if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_command_effect WHERE "key" = $1`, key).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != want {
				t.Errorf("markers for %s after purge = %d, want %d", key, count, want)
			}
		}
	}
}
