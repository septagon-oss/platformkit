package idempotency_test

import (
	"context"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/idempotency"
)

func TestExpiredCompletedKeyCanBeClaimedBeforePurge(t *testing.T) {
	for name, build := range map[string]func(*testing.T) (idempotency.Store, context.Context){
		"memory": inMemory, "postgres": inPostgres,
	} {
		t.Run(name, func(t *testing.T) {
			store, ctx := build(t)
			s := scope("expired-completed")
			first, err := store.Claim(ctx, s, start)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Complete(ctx, s, first.Claim, idempotency.Answer{Status: 201, Body: []byte("old")}, start); err != nil {
				t.Fatal(err)
			}
			later := start.Add(idempotency.Retention + time.Second)
			s.Fingerprint = sum("new body after retention")
			fresh, err := store.Claim(ctx, s, later)
			if err != nil {
				t.Fatal(err)
			}
			if fresh.Kind != idempotency.First {
				t.Fatalf("expired completed key verdict = %v, want First before scheduled purge", fresh.Kind)
			}
			if err := store.Complete(ctx, s, fresh.Claim, idempotency.Answer{Status: 201, Body: []byte("new")}, later); err != nil {
				t.Fatal(err)
			}
			replay, err := store.Claim(ctx, s, later)
			if err != nil || replay.Kind != idempotency.Replay || string(replay.Stored.Body) != "new" {
				t.Errorf("new write replay = %+v, %v; want new answer", replay, err)
			}
		})
	}
}
