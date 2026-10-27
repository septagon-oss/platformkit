package internal_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// TestASystemReadOfTheMailRecordSeesEveryTenant asks the mail record what it holds of two
// tenants, first as one tenant and then as the installation. The tenant sees only its own
// row; the installation, which sets no tenant between the count and the policy, sees both.
//
// A claim that a door "left no record" is read either way, and the two readings are not the
// same question: a policy is silent about a row it hides, so a count as one tenant answers
// only for rows that tenant may see. The reading that costs nothing to lose is the one that
// can be told from the other, so this says which is which.
func TestASystemReadOfTheMailRecordSeesEveryTenant(t *testing.T) {
	_, conn := dbtest.Schema(t, notificationmodule.Migrations)
	ledger := mailLedger()
	for _, tenant := range []tenancy.Tenant{acme, globex} {
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return ledger.RecordMail(ctx, tx, notification.MailRecord{
				Kind: contracts.MailSetPassword, Recipient: "ada@" + tenant.Slug + ".localhost",
				Outcome: notification.MailSent, RequestID: "one-record-per-tenant-" + tenant.Slug,
			})
		})
		if err != nil {
			t.Fatalf("record %s's mail: %v", tenant.Slug, err)
		}
	}
	own := map[string]int64{}
	for _, tenant := range []tenancy.Tenant{acme, globex} {
		var rows int64
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Raw("SELECT count(*) FROM direct_mail_deliveries").Row().Scan(&rows)
		})
		if err != nil {
			t.Fatalf("count as %s: %v", tenant.Slug, err)
		}
		own[tenant.Slug] = rows
	}
	all := int64(-1)
	err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw("SELECT count(*) FROM direct_mail_deliveries").Row().Scan(&all)
	})
	if err != nil {
		t.Fatalf("count as the installation: %v", err)
	}
	if own["acme"] != 1 || own["globex"] != 1 {
		t.Errorf("the tenants counted %d and %d records of their own, want 1 each: one tenant must not read the other's",
			own["acme"], own["globex"])
	}
	if all != 2 {
		t.Errorf("the installation counted %d records where two tenants hold one each: a count with no tenant set must answer for every row in the table, whatever id carries it",
			all)
	}
}
