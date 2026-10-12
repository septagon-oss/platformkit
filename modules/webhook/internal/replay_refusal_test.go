package internal_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

func TestDisabledReplayReturnsNoRowAndChangesNothing(t *testing.T) {
	e := newEnv(t, false, nil, nil)
	actor := uuid.New()
	endpoint, _ := e.register(acme, actor, "https://receiver.example.invalid/events", "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.in(acme, actor, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := e.svc.Disable(ctx, tx, endpoint.ID)
		return err
	})
	before := e.deliveries(acme, endpoint.ID)
	publications := e.outboxNames(acme)
	var returned *contracts.Delivery
	err := e.inErr(acme, actor, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		returned, err = e.svc.Replay(ctx, tx, before[0].ID)
		return err
	})
	if !errors.Is(err, crud.ErrConflict) || returned != nil {
		t.Fatalf("disabled replay returned row=%+v error=%v; want no row and conflict", returned, err)
	}
	after := e.deliveries(acme, endpoint.ID)
	if len(after) != len(before) || after[0].ID != before[0].ID || after[0].State != before[0].State || !after[0].UpdatedAt.Equal(before[0].UpdatedAt) {
		t.Errorf("refused replay changed its log: before=%v after=%v", state(before), state(after))
	}
	if got := e.outboxNames(acme); !slices.Equal(got, publications) {
		t.Errorf("refused replay published: before=%v after=%v", publications, got)
	}
}
