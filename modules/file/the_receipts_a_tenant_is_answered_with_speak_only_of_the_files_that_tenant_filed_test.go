package file_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestTheReceiptsATenantIsAnsweredWithSpeakOnlyOfTheFilesThatTenantFiled asks that
// the receipts a tenant's subscribers are handed speak of that tenant's files, in
// the words of the payload and not only in the tenant the row is filed against.
//
// A subscriber reads a receipt's payload rather than reading files back, so what
// crosses a tenant boundary when row-level security slips is the payload's own
// words: the file id, the name and the media type of the object another tenant
// filed. A row's owner column and a count of the rows naming one file id are both
// silent about a payload that names a stranger's file, so this reads the delivery
// table unfiltered from each filing tenant's own transaction, where the policy
// binds, and asks of the rows it is answered with: the file id this tenant filed
// appears, the other tenant's file id appears nowhere, and the name the other
// tenant filed its object under appears in no payload at all.
func TestTheReceiptsATenantIsAnsweredWithSpeakOnlyOfTheFilesThatTenantFiled(t *testing.T) {
	router, _, conn := mountedOn(t, map[string]tenancy.Tenant{host: acme, otherHost: globex})
	filings := []struct {
		id, name string
		filed    tenancy.Tenant
	}{
		{id: uploadAs(t, router, host, files, "the-name-acme-filed.txt", "text/plain",
			[]byte("the bytes one tenant filed\n")), name: "the-name-acme-filed.txt", filed: acme},
		{id: uploadAs(t, router, otherHost, files, "the-name-globex-filed.txt", "text/plain",
			[]byte("the bytes the other tenant filed\n")), name: "the-name-globex-filed.txt", filed: globex},
	}

	for at, watching := range filings {
		stranger := filings[1-at]
		var ownID, otherID, otherName, ownName int
		if err := db.Run(tenancy.WithTenant(t.Context(), watching.filed), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return tx.DB().Raw(`SELECT count(*) FILTER (WHERE payload->>'fileId' = ?),
						count(*) FILTER (WHERE payload->>'fileId' = ?),
						count(*) FILTER (WHERE payload->>'name' = ?),
						count(*) FILTER (WHERE payload->>'name' = ?)
					FROM `+outbox, watching.id, stranger.id, stranger.name, watching.name).
					Row().Scan(&ownID, &otherID, &otherName, &ownName)
			}); err != nil {
			t.Fatalf("reading the delivery rows %s is answered with: %v", watching.filed.Slug, err)
		}
		if ownID != 1 {
			t.Errorf("%s is answered with %d receipts naming %s, want its own upload answered once",
				watching.filed.Slug, ownID, watching.id)
		}
		if otherID != 0 {
			t.Errorf("%s is handed a receipt naming %s, a file it never filed",
				watching.filed.Slug, stranger.id)
		}
		if otherName != 0 {
			t.Errorf("%s is handed a receipt whose payload names the file %q, which %s filed",
				watching.filed.Slug, stranger.name, stranger.filed.Slug)
		}
		if ownName != 1 {
			t.Errorf("%s is answered with %d receipts naming its own file %q, want one",
				watching.filed.Slug, ownName, watching.name)
		}
	}
}
