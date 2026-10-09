package internal_test

import (
	"context"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/content"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/content/internal"
)

func TestConcurrentPublishEmitsOnce(t *testing.T) {
	admin, conn := dbtest.Schema(t, content.Migrations)
	svc := internal.NewService()
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 5*time.Second)
	defer cancel()
	page := &contracts.Content{Slug: "same-page", Title: "Same page", Body: "## Heading"}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return crud.Create(ctx, tx, page)
	}); err != nil {
		t.Fatal(err)
	}

	firstReady := make(chan int, 1)
	releaseFirst := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(releaseFirst)
		}
	}()
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if _, err := svc.Publish(ctx, tx, page.ID); err != nil {
				return err
			}
			var pid int
			if err := tx.DB().Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
				return err
			}
			firstReady <- pid
			<-releaseFirst
			return nil
		})
	}()
	var holderPID int
	select {
	case holderPID = <-firstReady:
	case err := <-firstResult:
		t.Fatalf("first publish: %v", err)
	case <-ctx.Done():
		t.Fatal("first publish did not reach its transaction hold")
	}

	secondResult := make(chan error, 1)
	go func() {
		secondResult <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.Publish(ctx, tx, page.ID)
			return err
		})
	}()
	for range time.Tick(5 * time.Millisecond) {
		var blocked bool
		if err := admin.QueryRowContext(ctx,
			"SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))", holderPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-secondResult:
			t.Fatalf("second publish finished before the first committed: %v", err)
		case <-ctx.Done():
			t.Fatal("second publish did not contend on the row")
		default:
		}
	}
	close(releaseFirst)
	released = true
	if err := <-firstResult; err != nil {
		t.Fatal(err)
	}
	if err := <-secondResult; err != nil {
		t.Fatal(err)
	}
	var count int
	if err := admin.QueryRowContext(t.Context(),
		"SELECT count(*) FROM platformkit_outbox WHERE name = $1 AND tenant_id = $2",
		contracts.EventPublished, acme.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("same page was published %d times, want one committed event", count)
	}
}
