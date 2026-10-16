package internal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

// TestTwoFirstAnswersForOneChannelLeaveOneRow: two requests that both find no
// row for (person, intent, channel) and both insert. The second waits on the
// first's index entry; when the first commits, the second either commits an
// update of the same row or is refused with a conflict. Either way the table
// holds one answer, it is the answer of the last request that succeeded, and
// one event was published per success.
func TestTwoFirstAnswersForOneChannelLeaveOneRow(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	person := notificationtest.Ada
	ctx, cancel := context.WithTimeout(tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), acme), tenancy.Principal{UserID: person}), 20*time.Second)
	defer cancel()
	prefs := notification.Settings()

	inserted := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := prefs.SetChannel(ctx, tx, person, "", contracts.ChannelEmail, false)
			close(inserted)
			if err != nil {
				return err
			}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	<-inserted
	second := make(chan error, 1)
	go func() {
		second <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := prefs.SetChannel(ctx, tx, person, "", contracts.ChannelEmail, true)
			return err
		})
	}()
	time.Sleep(300 * time.Millisecond) // the second reaches the index entry the first holds
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first answer: %v", err)
	}
	secondErr := <-second
	t.Logf("second answer after the first committed: %v", secondErr)
	if secondErr != nil && !errors.Is(secondErr, crud.ErrConflict) {
		t.Fatalf("second answer: got %v, want success or a conflict", secondErr)
	}

	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, err := (internal.Prefs{}).Settings(ctx, tx, person, "")
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			t.Fatalf("two first answers left %d rows, want one", len(rows))
		}
		want := secondErr == nil // the second's "on" wins only if it committed
		if rows[0].Enabled != want {
			t.Errorf("the row says enabled=%v, want %v (second refused: %v)", rows[0].Enabled, want, secondErr != nil)
		}
		published := 0
		for _, name := range outbox(t, tx) {
			if name == contracts.EventPreferenceSet {
				published++
			}
		}
		successes := 1
		if secondErr == nil {
			successes = 2
		}
		if published != successes {
			t.Errorf("published %d preference_set events for %d committed answers", published, successes)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
