package internal_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// A resent invitation kills the link it replaces the moment the resend commits,
// so a handoff of the new generation that the transport then refuses leaves the
// person no working link at all: the superseded credential was overwritten by
// the rotation and the new one is retired with its refusal. The two corners
// beside this reading are pinned — a generation-1 refusal, and two accepted
// handoffs — and the mixed pair between them, one send taken and the next
// refused, is what a reader of the trail and a pending-member screen both have
// to be able to tell apart. A build that leaves the refused mint live, that
// records the refusal as a second send, or that names a reason beside a send the
// transport took satisfies every other case in the package.
func TestAResentHandoffTheTransportRefusesLeavesNoLinkLive(t *testing.T) {
	f := invitationSQL(t)
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	superseded := authtest.InvitationOffer(t, f.InvitationFixture, u)
	f.Advance(contracts.ResetInterval)
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.Commands.ResendInvitation(ctx, tx, u.ID, u.Invitation.Revision)
		return err
	}); err != nil {
		t.Fatalf("the resend returned %v, want the current revision accepted", err)
	}
	var resent *usercontracts.User
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		resent, err = f.Users.Get(ctx, tx, u.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f.Mail.Failure = errors.New("smtp 451 4.3.0 the relay would not take it")
	refused := authtest.InvitationOffer(t, f.InvitationFixture, resent)
	sent := f.Mail.Sent()
	if len(sent) != 2 || superseded == "" || refused == "" {
		t.Fatalf("%d mailed links, %d of them carrying a credential, want the send the transport took and the resent handoff it refused",
			len(sent), len(minted(sent)))
	}

	var rows, taken, declined, reasons, events, agreeing, live int64
	var payloads string
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Raw(`SELECT count(*),
			count(*) FILTER (WHERE outcome = $2), count(*) FILTER (WHERE outcome = $3),
			count(*) FILTER (WHERE reason <> '')
			FROM direct_mail_deliveries WHERE kind = $1`,
			contracts.MailSetPassword, notification.MailSent, notification.MailFailed).
			Row().Scan(&rows, &taken, &declined, &reasons); err != nil {
			return err
		}
		if err := tx.DB().Raw(`SELECT count(*),
			count(*) FILTER (WHERE payload->>'reason' = $2 AND payload->>'kind' = $3),
			coalesce(string_agg(payload::text, '|'), '')
			FROM platformkit_outbox WHERE name = $1`,
			contracts.EventMailFailed, "transport_error", contracts.MailSetPassword).
			Row().Scan(&events, &agreeing, &payloads); err != nil {
			return err
		}
		return tx.DB().Raw("SELECT count(*) FROM password_tokens").Row().Scan(&live)
	}); err != nil {
		t.Fatal(err)
	}
	if rows != 2 || taken != 1 || declined != 1 {
		t.Errorf("%d records, %d taken and %d refused, want one handoff the transport took and the resent one it refused",
			rows, taken, declined)
	}
	if reasons != 1 {
		t.Errorf("%d of the %d records name a reason, want only the refused send to name why", reasons, rows)
	}
	if events != 1 || agreeing != 1 {
		t.Errorf("%d failure events, %d of them saying the record's own kind and word, want one of each: %s",
			events, agreeing, payloads)
	}
	for _, tok := range minted(sent) {
		if strings.Contains(payloads, tok) || strings.Contains(payloads, string(contracts.Hash(tok))) {
			t.Error("a failure event carries a credential this invitation's handoffs minted")
		}
	}
	if live != 0 {
		t.Errorf("%d credential rows beside a send that was taken and a resent handoff that was refused, want none", live)
	}
	for _, tok := range []string{superseded, refused} {
		err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := f.Auth.Reset(ctx, tx, tok, "a sufficiently strong password", contracts.Client{})
			return err
		})
		if !errors.Is(err, contracts.ErrCredentials) {
			t.Errorf("a mailed link of an invitation whose resent handoff was refused returned %v, want the door refused", err)
		}
	}
}

// minted answers how many of these mailed links carry a credential at all, which
// is the probe every record claim below rests on: a build that mailed nothing
// earns its ledger reading by nothing.
func minted(messages []notification.Message) []string {
	var tokens []string
	for _, m := range messages {
		if tok := authtest.TokenIn(m.Body); tok != "" {
			tokens = append(tokens, tok)
		}
	}
	return tokens
}
