package httpx_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestAKeyIsScopedToItsTenant: the same person, the same key and the same bytes
// in a second tenant is a second command. The tenant comes from the request's own
// resolution and is part of the claim, so the second tenant runs its command and
// is never handed the first tenant's stored answer.
func TestAKeyIsScopedToItsTenant(t *testing.T) {
	api, router, f, runs, path := setupNote(t)
	first := send(t, router, path, keyA, "one")
	if first.Code != http.StatusOK {
		t.Fatalf("the first tenant's command answered %d: %s", first.Code, first.Body)
	}
	firstTenant := f.tenant.ID
	f.tenant = tenancy.Tenant{ID: uuid.New(), Slug: "other", Name: "Other"}
	api.InvalidateHost(host)
	second := send(t, router, path, keyA, "one")
	if second.Code != http.StatusOK {
		t.Fatalf("the second tenant's command answered %d: %s", second.Code, second.Body)
	}
	var rows []uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), f.tenant), f.app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT tenant_id FROM notes ORDER BY id").Scan(&rows).Error
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0] != firstTenant || rows[1] != f.tenant.ID {
		t.Fatalf("two tenants under one key wrote notes for %v, want one for %s and one for %s (replay %q, runs %d)",
			rows, firstTenant, f.tenant.ID, second.Header().Get(httpx.IdempotencyReplayHeader), runs.Load())
	}
	if second.Header().Get(httpx.IdempotencyReplayHeader) == "true" {
		t.Fatal("the second tenant received the first tenant's stored response")
	}
}
