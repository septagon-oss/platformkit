package httpx_test

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type markerOutage struct {
	idempotency.Store
	calls atomic.Int32
}

func (s *markerOutage) Mark(ctx context.Context, tx db.Tx[db.Tenant], scope idempotency.Scope, claim uuid.UUID) error {
	s.calls.Add(1)
	return (brokenStore{}).Mark(ctx, tx, scope, claim)
}

func TestAMarkerFailureRollsBackTheCommand(t *testing.T) {
	const eventName = "signal.marker"
	release, err := events.DeclareMore([]events.Declared{events.Declare[keyedOut](eventName)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	store := &markerOutage{Store: idempotency.Postgres(httpx.ConnFrom)}
	api, router, _ := keyedSetup(t, store, nil)
	command := &commandEffect{path: "marker-unavailable"}
	id := uuid.New().String()
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "create-marked-signal", Method: http.MethodPost, Path: "/marker-unavailable",
		DefaultStatus: http.StatusCreated,
		Extensions:    map[string]any{httpx.EventsExtension: []string{eventName}},
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*struct{ Body keyedOut }, error) {
		command.conn, _ = httpx.ConnFrom(ctx)
		command.who, _ = tenancy.FromContext(ctx)
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, errStoreDown
		}
		if err := tx.DB().Exec("INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES (?, ?, ?)",
			uuid.New(), command.path, command.who.ID).Error; err != nil {
			return nil, err
		}
		out := keyedOut{ID: id}
		return &struct{ Body keyedOut }{Body: out}, events.Publish(ctx, tx, eventName, out)
	})
	response := keyedPost(t, router, http.MethodPost, ambiguousPath("marker-unavailable"), "marker-unavailable", ambiguousBody)
	if store.calls.Load() != 1 {
		t.Fatalf("marker calls = %d, want one", store.calls.Load())
	}
	if response.Code < http.StatusInternalServerError {
		t.Errorf("marker unavailable answered %d %s; want an unavailable outcome", response.Code, response.Body.String())
	}
	if rows := command.committedRows(t); rows != 0 {
		t.Errorf("marker unavailable committed %d command rows; want none", rows)
	}
	if strings.Contains(response.Body.String(), id) {
		t.Errorf("marker unavailable returned the uncommitted command row: %s", response.Body.String())
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), command.who), command.conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var count int64
		if err := tx.DB().Table("platformkit_outbox").Where("name = ?", eventName).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("marker unavailable committed %d outbox events, want none", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
