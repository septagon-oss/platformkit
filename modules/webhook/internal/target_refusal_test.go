package internal_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

func TestARefusedTargetReturnsNoRowAndChangesNothing(t *testing.T) {
	e := newEnv(t, false, nil, nil)
	actor := uuid.New()
	endpoint, _ := e.register(acme, actor, "https://hooks.example.com/original", "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.now))
	var beforeEndpoint *contracts.Endpoint
	e.in(acme, actor, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		beforeEndpoint, err = crud.Get[*contracts.Endpoint](tx, endpoint.ID)
		return err
	})
	beforeDeliveries := e.deliveries(acme, endpoint.ID)
	beforeOutbox := e.outboxNames(acme)
	for _, tc := range []struct {
		name   string
		tenant tenancy.Tenant
		url    string
		events contracts.EventNames
		want   error
	}{
		{"private destination", acme, "https://127.0.0.1/hooks", contracts.EventNames{"task.resolved"}, crud.ErrInvalid},
		{"unknown event", acme, "https://hooks.example.com/changed", contracts.EventNames{"task.unknown"}, crud.ErrInvalid},
		{"foreign endpoint", globex, "https://hooks.example.com/changed", contracts.EventNames{"task.resolved"}, crud.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var returned *contracts.Endpoint
			err := e.inErr(tc.tenant, actor, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				var err error
				returned, err = e.svc.Target(ctx, tx, endpoint.ID, tc.url, tc.events)
				return err
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("target refusal = %v, want %v", err, tc.want)
			}
			if returned != nil {
				t.Errorf("refused target returned a row: %+v", returned)
			}
			var saved *contracts.Endpoint
			e.in(acme, actor, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				var err error
				saved, err = crud.Get[*contracts.Endpoint](tx, endpoint.ID)
				return err
			})
			if !reflect.DeepEqual(saved, beforeEndpoint) {
				t.Errorf("refused target changed its endpoint: %+v", saved)
			}
			if got := e.deliveries(acme, endpoint.ID); !reflect.DeepEqual(got, beforeDeliveries) {
				t.Errorf("refused target changed pending deliveries: %v", state(got))
			}
			if got := e.outboxNames(acme); !slices.Equal(got, beforeOutbox) {
				t.Errorf("refused target published %v, want %v", got, beforeOutbox)
			}
			if got := e.outboxNames(globex); len(got) != 0 {
				t.Errorf("foreign refusal published %v", got)
			}
		})
	}
}
