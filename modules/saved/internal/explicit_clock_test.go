package internal_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/saved"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
	"github.com/septagon-oss/platformkit/modules/saved/contracts/savedtest"
	"github.com/septagon-oss/platformkit/modules/saved/internal"
)

// TestACopyCarriesTheInstantTheCompositionStampedIt keeps the stamp where the
// contract puts it. When the copy was taken is a fact about the installation that
// kept it, so the composition hands a clock down and the write answers with that
// instant — the snapshot's own, and the row's, which is what makes one person's
// copies come back newest first without the reader trusting a database default.
func TestACopyCarriesTheInstantTheCompositionStampedIt(t *testing.T) {
	_, conn := dbtest.Schema(t, saved.Migrations)
	stamped := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	grants := &desk{held: map[uuid.UUID][]string{}}
	keeper := uuid.New()
	grants.grant(keeper, contracts.PermissionRead, contracts.PermissionWrite)
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New(), Slug: "acme"}),
		tenancy.Principal{UserID: keeper})
	service := internal.NewService(grants, []contracts.Savable{{Module: "example", Kind: "thing", Source: &savedtest.Source{
		Value: contracts.Capture{Title: "Kept at five past six", Locale: "en-GB", Version: "v1",
			Values: json.RawMessage(`{"size":"red"}`)}}}})
	service.Clock = func() time.Time { return stamped }

	var held contracts.Item
	err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		held, err = service.Save(ctx, tx, contracts.Save{Source: oneSource, ExpectedSourceVersion: "v1"})
		return err
	})
	if err != nil {
		t.Fatalf("the first save: %v", err)
	}
	if !held.Snapshot.CapturedAt.Equal(stamped) {
		t.Errorf("the snapshot was taken at %s, want the instant the composition named", held.Snapshot.CapturedAt)
	}
	var back contracts.View
	err = db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		back, err = service.Get(ctx, tx, held.ID.String())
		return err
	})
	if err != nil {
		t.Fatalf("read the copy back: %v", err)
	}
	if !back.CreatedAt.Equal(stamped) || !back.UpdatedAt.Equal(stamped) {
		t.Errorf("the row carries %s and %s, both written by the clock named", back.CreatedAt, back.UpdatedAt)
	}
}
