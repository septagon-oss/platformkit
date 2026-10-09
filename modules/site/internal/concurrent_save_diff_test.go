package internal_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/site"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/site/internal"
)

// TestAConcurrentSaveDiffsAgainstTheValueItReplaced holds Save's FOR UPDATE: two
// saves race on one row, the second waits for the first's lock, and the second's
// event names the first's value as the one it replaced — never the value both read
// before either committed.
func TestAConcurrentSaveDiffsAgainstTheValueItReplaced(t *testing.T) {
	admin, conn := dbtest.Schema(t, site.Migrations)
	ctx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New()})
	svc := internal.NewService()
	save := func(ctx context.Context, tagline string) error {
		return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.Save(ctx, tx, &contracts.SiteSettings{Title: "Race", Tagline: tagline})
			return err
		})
	}
	if err := save(ctx, "first"); err != nil {
		t.Fatal(err)
	}

	held, release := make(chan struct{}), make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if _, err := svc.Save(ctx, tx, &contracts.SiteSettings{Title: "Race", Tagline: "second"}); err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	secondDone := make(chan error, 1)
	go func() { secondDone <- save(ctx, "third") }()

	// The second save is queued behind the first's row lock before the first commits.
	deadline := time.Now().Add(30 * time.Second)
	for {
		var waiting int
		if err := admin.QueryRowContext(t.Context(),
			`SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query ILIKE '%site_settings%FOR UPDATE%'`).
			Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("the second save never queued behind the first's lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}

	var last string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT payload::text FROM platformkit_outbox ORDER BY created_at DESC, id LIMIT 1`).Scan(&last); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(last, `"before": "second"`) || !strings.Contains(last, `"after": "third"`) {
		t.Errorf("the save that replaced \"second\" does not say so: %s", last)
	}
}
