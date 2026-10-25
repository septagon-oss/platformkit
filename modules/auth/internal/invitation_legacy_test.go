package internal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestLegacyInvitationRetainsItsLiveCredentialWithoutInventingDelivery(t *testing.T) {
	f := invitationSQL(t)
	u := &user.User{Email: "legacy@example.com", Status: user.StatusInvited}
	token := uuid.NewString()
	expires := f.clock.Now().Add(10 * time.Minute)
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := crud.Create(ctx, tx, u); err != nil {
			return err
		}
		return tx.DB().Exec("INSERT INTO password_tokens (tenant_id,user_id,token_hash,created_at,expires_at) VALUES (?,?,?,?,?)", db.TenantOf(tx).ID, u.ID, contracts.Hash(token), f.clock.Now().Add(-50*time.Minute), expires).Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.service.GetInvitationReading(ctx, tx, u.ID)
		return err
	}); !errors.Is(err, user.ErrInvitationInitializing) {
		t.Fatalf("unadopted reading = %v", err)
	}
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := f.Users.(contracts.InvitationUsers).LockInvitation(ctx, tx, u.ID); err != nil {
			return err
		}
		return f.Auth.Offer(ctx, tx, u.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if len(f.Mail.Sent()) != 0 {
		t.Fatal("legacy live token mailed a replacement")
	}
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		r, err := f.service.GetInvitationReading(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		if r.Request != "unrecorded" || r.Observation != "unknown" || !r.Legacy {
			t.Fatalf("legacy reading: %+v", r)
		}
		var got time.Time
		if err := tx.DB().Table("password_tokens").Where("user_id = ?", u.ID).Pluck("expires_at", &got).Error; err != nil {
			return err
		}
		if !got.Equal(expires) {
			t.Fatalf("legacy expiry moved: %v", got)
		}
		_, err = f.Auth.Reset(ctx, tx, token, "a sufficiently strong password", nobody)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
func TestInvitationExpiryIsCheckedAfterWaitingForThePerson(t *testing.T) {
	f := invitationSQL(t)
	u := sqlInvite(t, f)
	token := sqlOffer(t, f, u)
	locked, release := make(chan struct{}), make(chan struct{})
	held, done := make(chan error, 1), make(chan error, 1)
	go func() {
		held <- f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if _, err := f.Users.(contracts.InvitationUsers).LockInvitation(ctx, tx, u.ID); err != nil {
				return err
			}
			close(locked)
			<-release
			f.Advance(contracts.TokenLifetime)
			return nil
		})
	}()
	select {
	case <-locked:
	case <-time.After(5 * time.Second):
		t.Fatal("lock unavailable")
	}
	go func() {
		done <- f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := f.Auth.Reset(ctx, tx, token, "a sufficiently strong password", nobody)
			return err
		})
	}()
	close(release)
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, contracts.ErrCredentials) {
		t.Fatalf("expired while waiting: %v", err)
	}
}
func TestInvitationBackwardClockRefusesWithoutAHandoff(t *testing.T) {
	f := invitationSQL(t)
	u := sqlInvite(t, f)
	f.Advance(-time.Second)
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return f.Links.OfferInvitation(ctx, tx, u.ID, 1, *u.Invitation.DeliveryID)
	}); !errors.Is(err, user.ErrInvitationUnavailable) {
		t.Fatalf("backward clock = %v", err)
	}
	if sent := len(f.Mail.Sent()); sent != 0 {
		t.Fatalf("a clock that moved backward caused %d handoffs", sent)
	}
}
