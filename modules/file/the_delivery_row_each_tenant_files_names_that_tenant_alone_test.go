package file_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheDeliveryRowEachTenantFilesNamesThatTenantAlone asks whose events a
// subscriber is handed when two tenants file through one door.
//
// The delivery table is tenant-owned like every other (migrations/000002_outbox),
// so a receipt is worth reading only if it belongs to the tenant that filed the
// frame: a row filed against the wrong tenant reaches that tenant's subscribers
// and leaves the one that asked without its upload, and a row one tenant can read
// beside another's hands out a file id, a name and a digest that were never
// theirs. Each upload below is read twice — as the installation, to ask which
// tenant the row names, and from the filing tenant's own transaction, where the
// policy decides what its subscribers can see. A receipt filed against the wrong
// tenant is refused by the first read, and a delivery table that leaks across is
// refused by the second.
func TestTheDeliveryRowEachTenantFilesNamesThatTenantAlone(t *testing.T) {
	router, admin, conn := mountedOn(t, map[string]tenancy.Tenant{host: acme, otherHost: globex})
	filings := []struct {
		id  string
		who tenancy.Tenant
	}{
		{uploadAs(t, router, host, files, "what-acme-filed.txt", "text/plain",
			[]byte("the bytes one tenant filed\n")), acme},
		{uploadAs(t, router, otherHost, files, "what-globex-filed.txt", "text/plain",
			[]byte("the bytes the other tenant filed\n")), globex},
	}

	for _, filing := range filings {
		var names []string
		var owner uuid.UUID
		rows, err := admin.QueryContext(t.Context(),
			`SELECT name, tenant_id FROM `+outbox+` WHERE payload->>'fileId' = $1`, filing.id)
		if err != nil {
			t.Fatalf("reading the delivery rows that name %s: %v", filing.id, err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name, &owner); err != nil {
				rows.Close()
				t.Fatalf("reading a delivery row that names %s: %v", filing.id, err)
			}
			names = append(names, name)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatalf("reading the delivery rows that name %s: %v", filing.id, err)
		}
		if len(names) != 1 || names[0] != contracts.EventUploaded {
			t.Fatalf("%s filed %v, want the one %q row naming %s",
				filing.who.Slug, names, contracts.EventUploaded, filing.id)
		}
		if owner != filing.who.ID {
			t.Errorf("the delivery row naming %s, which %s filed, names tenant %s", filing.id, filing.who.Slug, owner)
		}
	}

	// The same table read from each tenant's own transaction, where row-level
	// security binds: what a tenant's subscribers can read is their own receipt,
	// and the file another tenant filed is not a row they are answered with at all.
	for at, watching := range filings {
		var own, other int
		if err := db.Run(tenancy.WithTenant(t.Context(), watching.who), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				if err := tx.DB().Raw(`SELECT count(*) FROM `+outbox+
					` WHERE payload->>'fileId' = ?`, watching.id).Row().Scan(&own); err != nil {
					return err
				}
				return tx.DB().Raw(`SELECT count(*) FROM `+outbox+
					` WHERE payload->>'fileId' = ?`, filings[1-at].id).Row().Scan(&other)
			}); err != nil {
			t.Fatalf("reading the delivery table as %s: %v", watching.who.Slug, err)
		}
		if own != 1 {
			t.Errorf("%s reads its own receipt %d times, want its upload answered with one row", watching.who.Slug, own)
		}
		if other != 0 {
			t.Errorf("%s is answered with a delivery row naming %s, a file it never filed", watching.who.Slug, filings[1-at].id)
		}
	}
}
