package internal_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

// TestTheLanguageRevisionIsAbsentOrMoves: house rule 7 — a column nothing reads is
// not written. users.locale_revision either does not exist, or a changed choice
// moves it, which is the only way a client could ever read it as a revision.
func TestTheLanguageRevisionIsAbsentOrMoves(t *testing.T) {
	conn, ctx := tenantWith(t)
	svc := newService()
	ada := invite(t, conn, "ada@acme.example.com")

	if err := db.Run(asSomeone(ctx, ada), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var columns int64
		if err := tx.DB().Raw(`SELECT count(*) FROM information_schema.columns
			WHERE table_name = 'users' AND column_name = 'locale_revision'`).Scan(&columns).Error; err != nil {
			return err
		}
		if _, err := svc.SetPreferredLocale(ctx, tx, "pt-PT"); err != nil {
			return err
		}
		if columns == 0 {
			return nil
		}
		var revision int64
		if err := tx.DB().Raw(`SELECT locale_revision FROM users WHERE id = ?`, ada).Scan(&revision).Error; err != nil {
			return err
		}
		if revision == 0 {
			t.Errorf("users.locale_revision is still 0 after a changed choice: the migration adds a column no command writes or reads")
		}
		return nil
	}); err != nil {
		t.Fatalf("SetPreferredLocale: %v", err)
	}
}
