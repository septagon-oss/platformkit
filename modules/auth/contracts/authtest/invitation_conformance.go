package authtest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// InvitationFixture runs each command in a fresh transaction in SQL; the fake
// has no rollback or durable commit model. Those properties need SQL-only cases.
type InvitationFixture struct {
	Step     func(func(context.Context, db.Tx[db.Tenant]) error) error
	Users    user.Service
	Commands user.InvitationCommands
	Links    contracts.InvitationLinks
	Auth     contracts.Service
	Mail     *InvitationMailbox
	Advance  func(time.Duration)
}

// InvitationMailbox records attempted handoffs, including failed ones.
type InvitationMailbox struct {
	Mailbox
	Failure error
}

func (m *InvitationMailbox) Send(ctx context.Context, msg notification.Message) error {
	_ = m.Mailbox.Send(ctx, msg)
	return m.Failure
}

// RunInvitationLinks holds the fake and SQL adapters to the same observable rules.
func RunInvitationLinks(t *testing.T, h func(*testing.T, func(InvitationFixture))) {
	cases := map[string]func(*testing.T, InvitationFixture){
		"replacement invalidates old link before another send":    invitationReplacement,
		"revoke fences acceptance and queued handoff":             invitationRevocation,
		"expiry refuses acceptance and permits explicit recovery": invitationExpiry,
		"weak password preserves the usable invitation":           invitationWeakPassword,
		"a failed send leaves no usable credential":               invitationFailedHandoff,
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) { h(t, func(f InvitationFixture) { run(t, f) }) })
	}
}

// InvitationPerson creates the person every invitation case begins with: an
// address, the roles the invitation asks for, nothing decided yet.
func InvitationPerson(t *testing.T, f InvitationFixture) *user.User {
	t.Helper()
	var u *user.User
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		u, err = f.Commands.CreateInvitation(ctx, tx, user.InvitationIntent{Email: "pending@example.com", Roles: []string{"editor"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return u
}

// InvitationOffer asks for one handoff of the person's current invitation
// generation and answers with the credential its mailed link carries, or "" when
// no handoff was recorded.
func InvitationOffer(t *testing.T, f InvitationFixture, u *user.User) string {
	t.Helper()
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return f.Links.OfferInvitation(ctx, tx, u.ID, u.Invitation.Generation, *u.Invitation.DeliveryID)
	}); err != nil {
		t.Fatal(err)
	}
	sent := f.Mail.Sent()
	if len(sent) == 0 {
		return ""
	}
	return TokenIn(sent[len(sent)-1].Body)
}
func invitationReset(f InvitationFixture, token, password string) error {
	return f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.Auth.Reset(ctx, tx, token, password, contracts.Client{})
		return err
	})
}
func invitationSnapshot(t *testing.T, f InvitationFixture, u *user.User) *user.User {
	t.Helper()
	var got *user.User
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		got, err = f.Users.Get(ctx, tx, u.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return got
}
func invitationReplacement(t *testing.T, f InvitationFixture) {
	u := InvitationPerson(t, f)
	old := InvitationOffer(t, f, u)
	f.Advance(contracts.ResetInterval)
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.Commands.ResendInvitation(ctx, tx, u.ID, 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := invitationReset(f, old, "a sufficiently strong password"); !errors.Is(err, contracts.ErrCredentials) {
		t.Fatalf("obsolete link = %v", err)
	}
	if got := invitationSnapshot(t, f, u); got.Status != user.StatusInvited || got.Invitation.Generation != 2 {
		t.Fatalf("after obsolete link: %+v", got)
	}
	InvitationOffer(t, f, u)
	if len(f.Mail.Sent()) != 1 {
		t.Fatal("obsolete worker sent mail")
	}
	current := invitationSnapshot(t, f, u)
	fresh := InvitationOffer(t, f, current)
	InvitationOffer(t, f, current)
	if len(f.Mail.Sent()) != 2 {
		t.Fatal("generation sent more than once")
	}
	if err := invitationReset(f, fresh, "a sufficiently strong password"); err != nil {
		t.Fatal(err)
	}
	got := invitationSnapshot(t, f, u)
	if got.Status != user.StatusActive || got.Invitation.Phase != user.InvitationAccepted || got.Invitation.AcceptedVia == nil || *got.Invitation.AcceptedVia != user.InvitationPasswordLink {
		t.Fatalf("acceptance: %+v", got.Invitation)
	}
	if err := invitationReset(f, fresh, "another sufficiently strong password"); !errors.Is(err, contracts.ErrCredentials) {
		t.Fatalf("spent link = %v", err)
	}
}
func invitationRevocation(t *testing.T, f InvitationFixture) {
	u := InvitationPerson(t, f)
	token := InvitationOffer(t, f, u)
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.Commands.RevokeInvitation(ctx, tx, u.ID, 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := invitationReset(f, token, "a sufficiently strong password"); !errors.Is(err, contracts.ErrCredentials) {
		t.Fatalf("revoked link = %v", err)
	}
	InvitationOffer(t, f, u)
	if len(f.Mail.Sent()) != 1 {
		t.Fatal("revoked worker sent mail")
	}
	got := invitationSnapshot(t, f, u)
	if got.Status != user.StatusInactive || got.PasswordHash != "" || got.Invitation.Phase != user.InvitationRevoked {
		t.Fatalf("revocation: %+v", got)
	}
}
func invitationExpiry(t *testing.T, f InvitationFixture) {
	u := InvitationPerson(t, f)
	token := InvitationOffer(t, f, u)
	f.Advance(contracts.TokenLifetime)
	if err := invitationReset(f, token, "a sufficiently strong password"); !errors.Is(err, contracts.ErrCredentials) {
		t.Fatalf("expired link = %v", err)
	}
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.Commands.ResendInvitation(ctx, tx, u.ID, 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	fresh := InvitationOffer(t, f, invitationSnapshot(t, f, u))
	if err := invitationReset(f, fresh, "a sufficiently strong password"); err != nil {
		t.Fatal(err)
	}
}
func invitationWeakPassword(t *testing.T, f InvitationFixture) {
	u := InvitationPerson(t, f)
	token := InvitationOffer(t, f, u)
	if err := invitationReset(f, token, "short"); !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("weak password = %v", err)
	}
	if got := invitationSnapshot(t, f, u); got.Invitation.Revision != 1 || got.PasswordHash != "" {
		t.Fatal("refused password changed invitation")
	}
	if err := invitationReset(f, token, "a sufficiently strong password"); err != nil {
		t.Fatal(err)
	}
}
func invitationFailedHandoff(t *testing.T, f InvitationFixture) {
	u := InvitationPerson(t, f)
	f.Mail.Failure = errors.New("provider secret must not be recorded")
	token := InvitationOffer(t, f, u)
	InvitationOffer(t, f, u)
	if len(f.Mail.Sent()) != 2 {
		t.Fatalf("a refused handoff caused %d attempts", len(f.Mail.Sent()))
	}
	for _, msg := range f.Mail.Sent() {
		if strings.Contains(msg.Body, "provider secret") {
			t.Fatal("the provider's failure reached the message")
		}
	}
	if err := invitationReset(f, token, "a sufficiently strong password"); !errors.Is(err, contracts.ErrCredentials) {
		t.Fatalf("failed send token = %v", err)
	}
	if got := invitationSnapshot(t, f, u); got.Invitation.Phase != user.InvitationPending || got.PasswordHash != "" {
		t.Fatalf("a refused handoff left %+v", got.Invitation)
	}
}
