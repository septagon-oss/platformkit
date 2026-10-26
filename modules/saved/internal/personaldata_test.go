package internal_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
	"github.com/septagon-oss/platformkit/modules/saved/internal"
)

// TestErasureTakesOnePersonsCopiesAndLeavesAnotherPersons answers the first
// sentence of the service contract: a copy survives until Unsave or personal-data
// erasure, and the erasure is the person's own. What the person asked to stop
// holding is gone from the table, what belongs to the member beside them is
// untouched, and the erasure announces nothing, because an outbox row naming a
// copy that no longer exists would tell every subscriber about a removal nobody
// asked for.
func TestErasureTakesOnePersonsCopiesAndLeavesAnotherPersons(t *testing.T) {
	service, conn, home, owner, held, grants := library(t)
	ownCtx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), home), tenancy.Principal{UserID: owner})
	strangerCtx, other := stranger(t, grants, home)
	if err := db.Run(strangerCtx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		_, err := other.Save(strangerCtx, tx, contracts.Save{Source: oneSource, ExpectedSourceVersion: "v1"})
		return err
	}); err != nil {
		t.Fatalf("the second member's own save: %v", err)
	}

	part := internal.PersonalData()
	if part.Module != "saved" || part.Data == nil {
		t.Fatalf("the contributed part: %+v", part)
	}
	var held_out json.RawMessage
	if err := db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		held_out, err = part.Data.Export(ctx, tx, personaldata.Subject{ID: owner})
		return err
	}); err != nil {
		t.Fatalf("export the person's own copies: %v", err)
	}
	if !strings.Contains(string(held_out), held.Snapshot.Title) || !strings.Contains(string(held_out), oneSource.Key) {
		t.Errorf("the export answered %s, which does not state the copy this person holds", held_out)
	}
	if strings.Contains(string(held_out), "another thing") {
		t.Errorf("the export reached another member's copy: %s", held_out)
	}

	if err := db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return part.Data.Erase(ctx, tx, personaldata.Subject{ID: owner})
	}); err != nil {
		t.Fatalf("erase the person's own copies: %v", err)
	}
	rows := witness(t, conn, ownCtx)
	if strings.Contains(rows, held.ID.String()) || strings.Contains(rows, held.Snapshot.Title) {
		t.Errorf("a copy survived its own erasure: %s", rows)
	}
	if !strings.Contains(rows, "another thing") {
		t.Errorf("the erasure took a copy that was not its subject's: %s", rows)
	}
	if got := published(t, conn, ownCtx, home); len(got) != 2 || got[0] != contracts.EventSaved || got[1] != contracts.EventSaved {
		t.Errorf("events after an erasure: %v, want the two saves and nothing else", got)
	}

	// The row is gone rather than hidden: an address that still resolves to a
	// removed copy is the one thing a taken-back copy must not leave behind.
	var back contracts.View
	err := db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		back, err = service.Get(ctx, tx, held.ID.String())
		return err
	})
	if err == nil || back.ID != uuid.Nil {
		t.Errorf("an erased copy is still readable: %+v %v", back.Item, err)
	}
}
