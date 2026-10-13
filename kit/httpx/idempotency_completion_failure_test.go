package httpx_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// completionOutage delegates claims to the real store, but simulates losing the
// database after the handler finishes. Moving the store's explicit time avoids a
// five-minute sleep while exercising its ordinary lease-expiration decision.
type completionOutage struct {
	idempotency.Store
	elapsed time.Duration
	failed  int
	conn    *db.Conn
	tenant  uuid.UUID
}

func (s *completionOutage) Claim(ctx context.Context, scope idempotency.Scope, at time.Time) (idempotency.Verdict, error) {
	s.conn, _ = httpx.ConnFrom(ctx)
	s.tenant = scope.Tenant
	return s.Store.Claim(ctx, scope, at.Add(s.elapsed))
}

func (s *completionOutage) Complete(context.Context, idempotency.Scope, uuid.UUID, idempotency.Answer, time.Time) error {
	s.failed++
	return errors.New("database unavailable while recording the completed answer")
}

func TestACompletedWriteIsNotRepeatedWhenRecordingItsAnswerFails(t *testing.T) {
	store := &completionOutage{Store: idempotency.Postgres(httpx.ConnFrom)}
	api, router, _ := keyedSetup(t, store, nil)
	// Use a tenant-owned kernel ledger as the transactional write. Each execution
	// inserts a fresh row, so a retry cannot hide behind a unique business key.
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "create-ledger-entry", Method: http.MethodPost, Path: "/ledger-entries",
		DefaultStatus: http.StatusCreated,
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, errors.New("request has no tenant transaction")
		}
		tenant, _ := tenancy.FromContext(ctx)
		err := tx.DB().Exec("INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES (?, ?, ?)", uuid.New(), "completed-write", tenant.ID).Error
		return &struct{}{}, err
	})
	const path = "/api/v1/public/probe/ledger-entries"
	first := keyedPost(t, router, http.MethodPost, path, "completed-write", "{}")
	if store.failed == 0 {
		t.Fatalf("first attempt never reached completion storage: status=%d body=%s", first.Code, first.Body.String())
	}
	store.elapsed = idempotency.Lease + time.Second
	retry := keyedPost(t, router, http.MethodPost, path, "completed-write", "{}")
	var committed int64
	ctx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: store.tenant, Slug: "acme"})
	if err := db.Run(ctx, store.conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("platformkit_handled").Where("durable = ?", "completed-write").Count(&committed).Error
	}); err != nil {
		t.Fatal(err)
	}
	if committed > 1 {
		t.Errorf("same write committed %d rows after completion storage failed; first status=%d retry status=%d; want at most one committed row throughout the 24-hour retention", committed, first.Code, retry.Code)
	}
}
