package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

func TestTargetCommitsItsDestinationAndAuditDiffTogether(t *testing.T) {
	e := newEnv(t, false, nil, nil)
	actor := uuid.New()
	endpoint, _ := e.register(acme, actor, "https://hooks.example.com/before", "task.assigned")
	beforeEvents := e.outboxNames(acme)
	const destination = "https://hooks.example.com/after"
	selected := contracts.EventNames{"task.assigned", "task.resolved"}
	abort := errors.New("caller cancels its transaction")
	err := e.inErr(acme, actor, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := e.svc.Target(ctx, tx, endpoint.ID, destination, selected); err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatalf("cancel target transaction: %v", err)
	}
	e.in(acme, actor, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		saved, err := crud.Get[*contracts.Endpoint](tx, endpoint.ID)
		if err != nil {
			return err
		}
		if saved.URL != endpoint.URL || !slices.Equal(saved.Events, endpoint.Events) {
			t.Errorf("rolled-back target changed destination: %s %v", saved.URL, saved.Events)
		}
		return nil
	})
	if got := e.outboxNames(acme); !slices.Equal(got, beforeEvents) {
		t.Fatalf("rolled-back target published %v, want %v", got, beforeEvents)
	}
	e.in(acme, actor, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		updated, err := e.svc.Target(ctx, tx, endpoint.ID, destination, selected)
		if err != nil {
			return err
		}
		if len(updated.Changes) != 0 {
			t.Error("target response retains the event's private audit diff")
		}
		return nil
	})
	e.in(acme, actor, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		saved, err := crud.Get[*contracts.Endpoint](tx, endpoint.ID)
		if err != nil {
			return err
		}
		if saved.URL != destination || !slices.Equal(saved.Events, selected) {
			t.Errorf("committed target lost destination: %s %v", saved.URL, saved.Events)
		}
		var payloads []string
		if err := tx.DB().Raw("SELECT payload::text FROM platformkit_outbox WHERE name = ?",
			contracts.EventEndpointUpdated).Scan(&payloads).Error; err != nil {
			return err
		}
		if len(payloads) != 1 {
			t.Fatalf("committed target published %d updates, want one", len(payloads))
		}
		var payload struct {
			ID      uuid.UUID        `json:"id"`
			Changes []map[string]any `json:"changes"`
		}
		if err := json.Unmarshal([]byte(payloads[0]), &payload); err != nil {
			return err
		}
		want := []map[string]any{
			{"field": "url", "before": endpoint.URL, "after": destination},
			{"field": "events", "before": []any{"task.assigned"}, "after": []any{"task.assigned", "task.resolved"}},
		}
		if payload.ID != endpoint.ID || !reflect.DeepEqual(payload.Changes, want) {
			t.Errorf("target audit = %s %v, want %s %v", payload.ID, payload.Changes, endpoint.ID, want)
		}
		return nil
	})
	committedEvents := e.outboxNames(acme)
	e.in(acme, actor, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := e.svc.Target(ctx, tx, endpoint.ID, destination, selected)
		return err
	})
	if got := e.outboxNames(acme); !slices.Equal(got, committedEvents) {
		t.Errorf("unchanged target published another audit event: %v", got)
	}
}
