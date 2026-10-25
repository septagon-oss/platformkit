package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// TestInvitationReadingProvesNoDelivery names the two halves the reading may
// answer from its own row: the handoff this generation asked for, and no more
// about the transport than "nothing was observed". The credential, the delivery
// identity and any password never appear in it.
func TestInvitationReadingProvesNoDelivery(t *testing.T) {
	f := invitationSQL(t)
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	token := authtest.InvitationOffer(t, f.InvitationFixture, u)
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		r, err := f.service.GetInvitationReading(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		if r.Request != "queued" || r.Observation != "unobserved" || r.State != user.InvitationPending {
			t.Fatalf("reading: %+v", r)
		}
		encoded, _ := json.Marshal(r)
		for _, secret := range []string{token, u.Invitation.DeliveryID.String(), "passwordHash"} {
			if strings.Contains(string(encoded), secret) {
				t.Fatalf("reading leaks %q", secret)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestInvitationAndOutboxRollbackTogether(t *testing.T) {
	f := invitationSQL(t)
	err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := f.Commands.CreateInvitation(ctx, tx, user.InvitationIntent{Email: "rollback@example.com", Roles: []string{"editor"}}); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatal(err)
	}
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for table, where := range map[string]string{"users": "email = 'rollback@example.com'", "platformkit_outbox": "name = 'user.invited'", "password_tokens": "true", "direct_mail_deliveries": "true"} {
			var count int64
			if err := tx.DB().Table(table).Where(where).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				t.Fatalf("%s contains %d rolled-back effects", table, count)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestAcceptanceAndRevocationSerializeOnThePerson(t *testing.T) {
	for _, acceptFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "revoke holds the row", true: "acceptance holds the row"}[acceptFirst], func(t *testing.T) {
			f := invitationSQL(t)
			u := authtest.InvitationPerson(t, f.InvitationFixture)
			token := authtest.InvitationOffer(t, f.InvitationFixture, u)
			accept := func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := f.Auth.Reset(ctx, tx, token, "a sufficiently strong password", nobody)
				return err
			}
			revoke := func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := f.Commands.RevokeInvitation(ctx, tx, u.ID, 1)
				return err
			}
			first, second := revoke, accept
			if acceptFirst {
				first, second = accept, revoke
			}
			locked, release := make(chan struct{}), make(chan struct{})
			firstDone, secondDone := make(chan error, 1), make(chan error, 1)
			go func() {
				firstDone <- f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					if _, err := f.Users.(contracts.InvitationUsers).LockInvitation(ctx, tx, u.ID); err != nil {
						return err
					}
					close(locked)
					<-release
					return first(ctx, tx)
				})
			}()
			select {
			case <-locked:
			case <-time.After(5 * time.Second):
				t.Fatal("first command did not lock")
			}
			started := make(chan struct{})
			go func() { close(started); secondDone <- f.Step(second) }()
			<-started
			close(release)
			if err := <-firstDone; err != nil {
				t.Fatal(err)
			}
			err := <-secondDone
			if acceptFirst && !errors.Is(err, crud.ErrConflict) {
				t.Fatalf("revoke after acceptance = %v", err)
			}
			if !acceptFirst && !errors.Is(err, contracts.ErrCredentials) {
				t.Fatalf("acceptance after revoke = %v", err)
			}
			if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				got, err := f.Users.Get(ctx, tx, u.ID)
				if err != nil {
					return err
				}
				want := user.InvitationRevoked
				if acceptFirst {
					want = user.InvitationAccepted
				}
				if got.Invitation.Phase != want || got.Invitation.Revision != 2 {
					t.Fatalf("race settled as %+v", got.Invitation)
				}
				var count int64
				if err := tx.DB().Table("platformkit_outbox").Where("name IN ?", []string{user.EventInvitationAccepted, user.EventInvitationRevoked}).Count(&count).Error; err != nil {
					return err
				}
				if count != 1 {
					t.Fatalf("race emitted %d transitions", count)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestForeignTenantCannotReadRevokeOrConsumeInvitation(t *testing.T) {
	f := invitationSQL(t)
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	token := authtest.InvitationOffer(t, f.InvitationFixture, u)
	ctx := tenancy.WithTenant(f.ctx, globex)
	if err := db.Run(ctx, f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := f.Commands.RevokeInvitation(ctx, tx, u.ID, 1)
		if row != nil {
			t.Fatal("foreign write returned row")
		}
		return err
	}); !errors.Is(err, crud.ErrNotFound) {
		t.Fatalf("foreign revoke = %v", err)
	}
	if err := db.Run(ctx, f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		r, err := f.service.GetInvitationReading(ctx, tx, u.ID)
		if r != nil {
			t.Fatal("foreign reading returned row")
		}
		return err
	}); !errors.Is(err, user.ErrInvitationForbidden) {
		t.Fatalf("foreign read = %v", err)
	}
	if err := db.Run(ctx, f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.Auth.Reset(ctx, tx, token, "a sufficiently strong password", nobody)
		return err
	}); !errors.Is(err, contracts.ErrCredentials) {
		t.Fatalf("foreign token = %v", err)
	}
}
