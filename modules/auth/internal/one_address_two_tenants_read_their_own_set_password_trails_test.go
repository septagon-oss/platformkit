package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// The set-password trail a delivery reading answers from is keyed by the address its
// link was named for, and two customers of one installation may name the same address
// to invite their own person. This asks that ledger's tenancy along the axis the cases
// beside it leave unasked — they put their two people in one tenant, or in two tenants
// under two addresses, and never one address in both. The same address is invited by
// each of two customers, and the transport takes the link mailed in one tenant and
// refuses the link mailed in the other. Each customer's own reading of that shared
// address then answers its own outcome alone: one row, sent where its send was taken,
// failed where its own refusal was recorded. A build that recorded one customer's
// trail into the other's rows, or whose read of that address reached past the tenant
// its transaction names, answers a person about a link their own customer never asked
// for and is refused here.
func TestOneAddressTwoTenantsReadTheirOwnSetPasswordTrails(t *testing.T) {
	const shared = "single.person@example.com"
	f := invitationSQL(t)
	globex := globexSession(t, f)
	abroad := invitedIn(t, f, globex, shared)
	refused := invitedIn(t, f, f.ctx, shared)
	if abroad.Invitation.Recipient != refused.Invitation.Recipient || abroad.Invitation.Recipient != shared {
		t.Fatalf("the two invitations were mailed to %q and %q, want the one address %s this asks the ledger about",
			abroad.Invitation.Recipient, refused.Invitation.Recipient, shared)
	}
	// Both people were invited, which is a recorded intent and no send, so a trail
	// here would already be a row no handoff of this address made.
	for _, read := range []struct {
		who  string
		ctx  context.Context
		rows string
	}{{"the tenant of " + refused.Email, f.ctx, ""}, {"the tenant of " + abroad.Email, globex, ""}} {
		if got := setPasswordTrailIn(t, f, read.ctx, shared); got != read.rows {
			t.Fatalf("%s reads %q before either link was mailed, want no set-password trail of %s yet",
				read.who, got, shared)
		}
	}
	if offeredIn(t, f, globex, abroad) == "" {
		t.Fatalf("no link was mailed to %s in the tenant that took the send, so no trail there could be read", shared)
	}
	f.Mail.Failure = errors.New("smtp 451 4.3.0 the relay would not take this tenant's mail")
	if offeredIn(t, f, f.ctx, refused) == "" {
		t.Fatalf("no link was minted for %s in the tenant that refused the send, so no refusal could be recorded there", shared)
	}
	f.Mail.Failure = nil
	for _, read := range []struct {
		who  string
		ctx  context.Context
		want string
	}{
		{"the tenant that took the send", globex, notification.MailSent},
		{"the tenant that refused the send", f.ctx, notification.MailFailed},
	} {
		if got := setPasswordTrailIn(t, f, read.ctx, shared); got != read.want {
			t.Errorf("%s reads the set-password trail of %s as %q, want its own %s and no row of the other customer's",
				read.who, shared, got, read.want)
		}
	}
}

// setPasswordTrailIn reads one tenant's own set-password outcomes for one address, in
// the order they were written, and answers "" when that tenant holds none: the read is
// bounded by the tenant its transaction names, which is the whole question here.
func setPasswordTrailIn(t *testing.T, f invitationSQLFixture, ctx context.Context, address string) string {
	t.Helper()
	var trail string
	if err := db.Run(ctx, f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT coalesce(string_agg(outcome, ',' ORDER BY seq), '')`+
			` FROM direct_mail_deliveries WHERE kind = $1 AND recipient = $2`,
			contracts.MailSetPassword, address).Row().Scan(&trail)
	}); err != nil {
		t.Fatal(err)
	}
	return trail
}
