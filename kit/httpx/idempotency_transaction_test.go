package httpx_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestConflictingKeyCommitsNoRowOrEventAndReturnsNoOldRow(t *testing.T) {
	const eventName = "signal.keyed"
	release, err := events.DeclareMore([]events.Declared{events.Declare[keyedOut](eventName)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	api, router, _ := keyedSetup(t, idempotency.Postgres(httpx.ConnFrom), nil)
	var conn *db.Conn
	var tenant tenancy.Tenant
	id := uuid.New()
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "create-keyed-signal", Method: http.MethodPost, Path: "/signals",
		DefaultStatus: http.StatusCreated,
		Extensions:    map[string]any{httpx.EventsExtension: []string{eventName}},
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*struct{ Body keyedOut }, error) {
		conn, _ = httpx.ConnFrom(ctx)
		tenant, _ = tenancy.FromContext(ctx)
		tx, _ := httpx.TxFrom(ctx)
		if err := tx.DB().Exec("INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES (?, ?, ?)", uuid.New(), eventName, tenant.ID).Error; err != nil {
			return nil, err
		}
		out := keyedOut{ID: id.String()}
		return &struct{ Body keyedOut }{Body: out}, events.Publish(ctx, tx, eventName, out)
	})
	const path = "/api/v1/public/probe/signals"
	first := keyedPost(t, router, http.MethodPost, path, "one-signal", `{"name":"first"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first = %d %s", first.Code, first.Body.String())
	}
	conflict := keyedPost(t, router, http.MethodPost, path, "one-signal", `{"name":"changed"}`)
	if conflict.Code != http.StatusUnprocessableEntity || !strings.Contains(conflict.Body.String(), httpx.CodeIdempotencyConflict) {
		t.Errorf("conflict = %d %s; want reasoned 422", conflict.Code, conflict.Body.String())
	}
	if strings.Contains(conflict.Body.String(), id.String()) {
		t.Error("conflict returned the original row")
	}
	replay := keyedPost(t, router, http.MethodPost, path, "one-signal", `{"name":"first"}`)
	if replay.Code != first.Code || replay.Body.String() != first.Body.String() {
		t.Errorf("original write no longer replays: %d %s", replay.Code, replay.Body.String())
	}
	err = db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		for table, column := range map[string]string{"platformkit_handled": "durable", "platformkit_outbox": "name"} {
			var count int64
			if err := tx.DB().Table(table).Where(column+" = ?", eventName).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				t.Errorf("%s rows = %d; want only the original write", table, count)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
