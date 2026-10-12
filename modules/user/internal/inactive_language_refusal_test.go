package internal_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/internal"
)

func TestDeactivatedPersonCannotChangeTheirLanguage(t *testing.T) {
	for _, counted := range []bool{false, true} {
		name := "intent"
		if counted {
			name = "counted"
		}
		t.Run(name, func(t *testing.T) {
			conn, ctx := tenantWith(t)
			svc := internal.NewService(&contracts.AdministrationFunc{Ask: administering}, allowGranting,
				speaksEnglishAndPortuguese)
			person := invite(t, conn, "language-actor@acme.example.com")
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return svc.SetPassword(ctx, tx, person, "language-fixture-password")
			}); err != nil {
				t.Fatal(err)
			}
			actor := asSomeone(ctx, person)
			if err := db.Run(actor, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := svc.SetPreferredLocaleAt(ctx, tx, 0, "en")
				return err
			}); err != nil {
				t.Fatalf("active person's choice: %v", err)
			}
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := svc.Deactivate(ctx, tx, person)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			before := len(outboxNames(t, conn))
			if err := db.Run(actor, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				var row *contracts.User
				var err error
				if counted {
					row, err = svc.SetPreferredLocaleAt(ctx, tx, 1, "pt-PT")
				} else {
					row, err = svc.SetPreferredLocale(ctx, tx, "pt-PT")
				}
				if err == nil {
					t.Error("deactivated actor's language change was allowed")
				}
				if row != nil {
					t.Errorf("refusal returned a user row with status=%q language=%q revision=%d", row.Status, row.PreferredLocale, row.LocaleRevision)
				}
				// Commit around the refusal to expose any mutation it left behind.
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				row, err := crud.Get[*contracts.User](tx, person)
				if err == nil && (row.Status != contracts.StatusInactive || row.PreferredLocale != "en" || row.LocaleRevision != 1) {
					t.Errorf("refusal left status=%q language=%q revision=%d", row.Status, row.PreferredLocale, row.LocaleRevision)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if after := len(outboxNames(t, conn)); after != before {
				t.Errorf("refusal emitted %d events", after-before)
			}
		})
	}
}
