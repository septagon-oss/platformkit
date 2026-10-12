package internal_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/site"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/site/contracts/sitetest"
	"github.com/septagon-oss/platformkit/modules/site/internal"
)

// TestTheTrailNeverNamesThePalettesAccent pins the half of the contract the
// conformance suite cannot see: the event a save publishes speaks stored values,
// so the installation's accent — nobody's edit — is never in a change line. A
// service built with a palette saves the read's answer back (stored as the empty
// colour), then changes the title; the second event names the title and nothing
// about the colour, and no payload carries either of the installation's accents.
// It fails if the resolution ever runs before the diff the event is made from.
func TestTheTrailNeverNamesThePalettesAccent(t *testing.T) {
	_, conn := dbtest.Schema(t, site.Migrations)
	ctx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New()})
	svc := internal.NewService(sitetest.InstallationPalette)
	light := sitetest.InstallationPalette.Accent(contracts.ThemeLight)
	dark := sitetest.InstallationPalette.Accent(contracts.ThemeDark)

	var payloads []string
	read := func(tx db.Tx[db.Tenant]) error {
		var lines []string
		if err := tx.DB().Raw("SELECT payload::text FROM platformkit_outbox ORDER BY created_at, id").Scan(&lines).Error; err != nil {
			return err
		}
		payloads = lines
		return nil
	}
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		shown, err := svc.Settings(ctx, tx)
		if err != nil {
			return err
		}
		if shown.PrimaryColor != light {
			t.Fatalf("the read answers %q, want the installation's %q", shown.PrimaryColor, light)
		}
		shown.Title = "Trail Works"
		if _, err := svc.Save(ctx, tx, shown); err != nil {
			return err
		}
		shown.Title = "Trail Works Limited"
		if _, err := svc.Save(ctx, tx, shown); err != nil {
			return err
		}
		return read(tx)
	})
	if err != nil {
		t.Fatalf("the saves: %v", err)
	}
	if len(payloads) != 2 {
		t.Fatalf("two saves that changed the title wrote %d events, want two", len(payloads))
	}
	for i, payload := range payloads {
		for _, accent := range []string{light, dark} {
			if strings.Contains(payload, accent) {
				t.Errorf("event %d names the installation's accent %s, which nobody chose: %s", i+1, accent, payload)
			}
		}
	}
	if !strings.Contains(payloads[1], `"title"`) || strings.Contains(payloads[1], `"primaryColor"`) {
		t.Errorf("the second event should say the title moved and nothing about the colour: %s", payloads[1])
	}
}
