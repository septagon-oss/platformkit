package internal_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/saved"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
	"github.com/septagon-oss/platformkit/modules/saved/contracts/savedtest"
	"github.com/septagon-oss/platformkit/modules/saved/internal"
)

// oneSource is the pair and key two racing callers both believe is unsaved.
var oneSource = contracts.Ref{Module: "example", Kind: "thing", Key: "racing"}

// TestTwoFirstSavesOfOneSourceSettleOnOneCopy settles the race the contract's
// idempotency depends on: two transactions that both believe this person has
// nothing yet save the same source at the same time. One row and one event is the
// answer whichever arrives first — the loser returns the winner's copy as its own
// idempotent success rather than a unique violation, because the caller asked for
// a state and got it.
func TestTwoFirstSavesOfOneSourceSettleOnOneCopy(t *testing.T) {
	_, conn := dbtest.Schema(t, saved.Migrations)
	source := &savedtest.Source{Value: contracts.Capture{
		Title: "The racing thing", Locale: "en-GB", Version: "v1",
		Values: json.RawMessage(`{"size":"red"}`),
	}}
	grants := &desk{held: map[uuid.UUID][]string{}}
	person := uuid.New()
	grants.grant(person, contracts.PermissionRead, contracts.PermissionWrite)
	home := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), home), tenancy.Principal{UserID: person})
	service := internal.NewService(grants, []contracts.Savable{{Module: "example", Kind: "thing", Source: source}})

	var wg sync.WaitGroup
	answers := make(chan contracts.Item, 2)
	failures := make(chan error, 2)
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			var row contracts.Item
			err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				var saveErr error
				row, saveErr = service.Save(ctx, tx, contracts.Save{Source: oneSource, ExpectedSourceVersion: "v1"})
				return saveErr
			})
			if err != nil {
				failures <- err
				return
			}
			answers <- row
		}()
	}
	wg.Wait()
	close(answers)
	close(failures)
	for err := range failures {
		t.Fatalf("a racing save was refused: %v", err)
	}
	var ids []uuid.UUID
	for row := range answers {
		ids = append(ids, row.ID)
	}
	if len(ids) != 2 {
		t.Fatalf("only %d of the two racing saves answered", len(ids))
	}
	if ids[0] != ids[1] {
		t.Fatalf("one source left two different copies: %v and %v", ids[0], ids[1])
	}

	// The witnesses read in a transaction of their own, so they see what the two
	// racing ones left behind rather than what either one believed.
	copies := count(t, conn, ctx, `SELECT count(*) FROM saved_items`)
	if copies != 1 {
		t.Fatalf("two first saves left %d copies", copies)
	}
	if events := published(t, conn, ctx, home); len(events) != 1 {
		t.Fatalf("two first saves published %v, want one saved.saved", events)
	}
}

// count asks one question of this tenant's visible rows.
func count(t *testing.T, conn *db.Conn, ctx context.Context, query string) int64 {
	t.Helper()
	var out int64
	err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(query).Scan(&out).Error
	})
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return out
}
