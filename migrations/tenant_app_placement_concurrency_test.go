package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestConcurrentPlacementCannotReplaceTheFirstCommittedApp(t *testing.T) {
	owner, _ := dbtest.Schema(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	id := uuid.New()
	if _, err := owner.ExecContext(ctx,
		`INSERT INTO tenants (id, slug, name, app) VALUES ($1, 'shop', 'Shop', '')`, id); err != nil {
		t.Fatal(err)
	}
	first, err := owner.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback()
	var unplaced string
	const place = `SELECT platformkit_place_tenants($1, '', $2, NULL)`
	if err := first.QueryRowContext(ctx, place, "collect", "shop=collect").Scan(&unplaced); err != nil {
		t.Fatal(err)
	}
	var holder string
	if err := first.QueryRowContext(ctx, `SELECT app FROM tenants WHERE id=$1`, id).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	if holder != "collect" || unplaced != "" {
		t.Fatalf("first placement: app=%q unplaced=%q, want collect and no unplaced tenant", holder, unplaced)
	}
	var firstPID int
	if err := first.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&firstPID); err != nil {
		t.Fatal(err)
	}
	second, err := owner.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	var secondPID int
	if err := second.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&secondPID); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		var left string
		finished <- second.QueryRowContext(ctx, place, "academy", "shop=academy").Scan(&left)
	}()
	// Observe this session waiting behind this writer, rather than assuming a
	// sleep put both declarations over the tenant's old empty value. A direct
	// refusal is also safe and must not depend on today's error sentence.
	var secondErr error
	completed := false
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
waiting:
	for {
		select {
		case secondErr = <-finished:
			completed = true
			break waiting
		default:
		}
		var queued bool
		if err := owner.QueryRowContext(ctx,
			`SELECT pg_blocking_pids($1::integer) @> ARRAY[$2::integer]`, secondPID, firstPID).Scan(&queued); err != nil {
			t.Fatal(err)
		}
		if queued {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("second placement neither completed nor reached the held row: ", ctx.Err())
		}
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	if !completed {
		select {
		case secondErr = <-finished:
		case <-ctx.Done():
			t.Fatal("second placement did not finish after the row was released: ", ctx.Err())
		}
	}
	if secondErr != nil {
		t.Logf("second placement refused: %v", secondErr)
	}
	if err := owner.QueryRowContext(ctx, `SELECT app FROM tenants WHERE id=$1`, id).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	if holder != "collect" {
		t.Errorf("the tenant's first committed app was replaced: app=%q, want collect; the waiting placement must not move a tenant between apps", holder)
	}
}
