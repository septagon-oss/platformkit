package internal_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
)

// publishedTXT implements the documented verifier: compare the public DNS TXT
// record at VerificationName with the server-issued challenge in Token.
type publishedTXT map[string]string

func (records publishedTXT) Verify(_ context.Context, s contracts.Sender) (string, error) {
	if records[s.VerificationName()] != s.Token || s.Token == "" {
		return "", fmt.Errorf("domain has not published this sender's challenge")
	}
	return "matching DNS TXT record", nil
}

func TestAnotherTenantCannotReuseAPublishedDomainProof(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	store := senders()
	store.Grants = grants{held: true}
	records := publishedTXT{}
	store.Verifier = records
	var publicToken string
	if err := db.Run(asAdmin(tenancy.WithTenant(t.Context(), acme)), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := store.Put(ctx, tx, sender())
		if err != nil {
			return err
		}
		publicToken = row.Token
		records[row.VerificationName()] = publicToken
		_, err = store.Verify(ctx, tx, row.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// DNS TXT values are public. The other tenant can read that value but
	// cannot publish a fresh challenge under the first tenant's domain.
	otherCtx := tenancy.WithPrincipal(
		tenancy.WithActor(tenancy.WithTenant(t.Context(), globex), notificationtest.Bob),
		tenancy.Principal{UserID: notificationtest.Bob},
	)
	if err := db.Run(otherCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		claim := sender()
		claim.Token = publicToken
		row, err := store.Put(ctx, tx, claim)
		if err != nil {
			// Refusing a supplied challenge is also safe, provided nothing changed.
			saved, readErr := store.For(ctx, tx)
			if row != nil || saved != nil || len(outbox(t, tx)) != 0 {
				t.Error("refused sender claim returned a row, wrote state or published an event")
			}
			return readErr
		}
		before := len(outbox(t, tx))
		verifiedRow, verifyErr := store.Verify(ctx, tx, row.ID)
		if verifyErr == nil || verifiedRow != nil {
			t.Errorf("another tenant reused a public DNS proof: Verify error=%v, returned row=%v; want refusal and no row", verifyErr, verifiedRow != nil)
		}
		if after := len(outbox(t, tx)); after != before {
			t.Errorf("refused verification published %d events, want zero", after-before)
		}
		saved, err := store.For(ctx, tx)
		if err == nil && saved.Status != contracts.SenderPending {
			t.Errorf("uncontrolled domain became %s in the other tenant, want pending", saved.Status)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
