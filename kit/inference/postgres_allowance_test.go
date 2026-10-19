package inference_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/inference"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestPostgresInferenceAllowanceIsTenantScopedAndRefusesWithoutEffects(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	store := limit.Postgres(func(context.Context) (*db.Conn, bool) { return conn, true })
	provider := inference.NewScript(inference.Reply{Text: "a cat", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}})
	engine, err := inference.New(inference.Config{Model: "test-model", DailyCalls: 2, Counter: store}, provider)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	a := tenancy.WithTenant(t.Context(), tenant)
	b := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New(), Slug: "other"})
	for range 2 {
		if _, err := engine.Chat(a, inference.Request{Prompt: "what is this?"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := engine.Chat(a, inference.Request{Prompt: "what is this?"})
	if !errors.Is(err, inference.ErrSpent) || got != (inference.Answer{}) {
		t.Fatalf("exhausted tenant = %+v, %v; want no answer and ErrSpent", got, err)
	}
	if n, _, err := store.Count(a, inference.Bucket, 24*time.Hour); err != nil || n != 2 {
		t.Fatalf("refusal changed the count: %d, %v", n, err)
	}
	if len(provider.Asks()) != 2 {
		t.Fatal("exhausted tenant reached the provider")
	}
	if n, _, err := store.Count(b, inference.Bucket, 24*time.Hour); err != nil || n != 0 {
		t.Fatalf("second tenant read the first tenant's counter: %d, %v", n, err)
	}
	if _, err := engine.Chat(b, inference.Request{Prompt: "what is this?"}); err != nil {
		t.Fatalf("second tenant spent the first tenant's allowance: %v", err)
	}
	if n, _, err := store.Count(a, inference.Bucket, 24*time.Hour); err != nil || n != 2 {
		t.Fatalf("second tenant changed the first tenant's counter: %d, %v", n, err)
	}
	var key string
	if err := admin.QueryRowContext(t.Context(), "SELECT key FROM platformkit_limits WHERE count = 2").Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(b, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var visible int64
		if err := tx.DB().Table("platformkit_limits").Where("key = ?", key).Count(&visible).Error; err != nil {
			return err
		}
		if visible != 0 {
			t.Errorf("tenant transaction read another tenant's row")
		}
		write := tx.DB().Table("platformkit_limits").Where("key = ?", key).Update("count", 0)
		if write.RowsAffected != 0 {
			t.Errorf("tenant transaction wrote another tenant's row")
		}
		return write.Error
	}); err != nil {
		t.Fatal(err)
	}
	var events int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("inference emitted %d undeclared events", events)
	}
}

func TestConcurrentPostgresCallsCannotBothSpendTheLastAllowance(t *testing.T) {
	_, conn := dbtest.Schema(t)
	store := limit.Postgres(func(context.Context) (*db.Conn, bool) { return conn, true })
	provider := inference.NewScript(inference.Reply{Text: "a cat", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}})
	engine, err := inference.New(inference.Config{Model: "test-model", DailyCalls: 1, Counter: store}, provider)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	ctx := tenancy.WithTenant(t.Context(), tenant)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			<-start
			got, err := engine.Chat(ctx, inference.Request{Prompt: "what is this?"})
			if err != nil && (!errors.Is(err, inference.ErrSpent) || got != (inference.Answer{})) {
				t.Errorf("contender = %+v, %v; want whole answer or empty spent refusal", got, err)
			}
		})
	}
	close(start)
	workers.Wait()
	if len(provider.Asks()) != 1 {
		t.Fatalf("the provider served %d calls at an allowance of one", len(provider.Asks()))
	}
}

func TestALockedInferenceCounterRefusesWithoutCallingTheProvider(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), tenant), conn)
	store := limit.Postgres(httpx.ConnFrom)
	if ok, _, err := store.Allow(ctx, inference.Bucket, 3, 24*time.Hour); err != nil || !ok {
		t.Fatalf("seed counter = %v, %v", ok, err)
	}
	var key string
	if err := admin.QueryRowContext(t.Context(), "SELECT key FROM platformkit_limits").Scan(&key); err != nil {
		t.Fatal(err)
	}
	release := dbtest.Hold(t, admin, 10*time.Second, "UPDATE platformkit_limits SET count = count WHERE key = $1", key)
	provider := inference.NewScript(inference.Reply{Text: "a cat", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}})
	engine, err := inference.New(inference.Config{Model: "test-model", DailyCalls: 3, Counter: store}, provider)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	got, err := engine.Chat(ctx, inference.Request{Prompt: "what is this?"})
	spent, ok := errors.AsType[*inference.SpentError](err)
	if !ok || !errors.Is(err, inference.ErrSpentUnproven) || spent.RetryAfter != 24*time.Hour || got != (inference.Answer{}) {
		t.Errorf("locked counter = %+v, %v; want empty unproven refusal with full window", got, err)
	}
	if len(provider.Asks()) != 0 {
		t.Error("locked counter admitted a provider call")
	}
	release()
	if n, _, err := store.Count(ctx, inference.Bucket, 24*time.Hour); err != nil || n != 1 {
		t.Fatalf("locked refusal changed the count: %d, %v", n, err)
	}
}
