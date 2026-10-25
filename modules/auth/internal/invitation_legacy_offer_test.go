package internal_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// invitationAdoptedPerson is the row the adoption file leaves behind: invited, with
// its history recorded from the address and no request ever made for it, so its
// generation is 0. The event that originally asked for its mail carries no
// generation either, because the build that wrote it had none.
func invitationAdoptedPerson(t *testing.T, f invitationSQLFixture) (*user.User, uuid.UUID) {
	t.Helper()
	u := &user.User{Email: "adopted@example.com", Status: user.StatusInvited}
	var tenant uuid.UUID
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		tenant = db.TenantOf(tx).ID
		if err := crud.Create(ctx, tx, u); err != nil {
			return err
		}
		_, err := f.Users.(contracts.InvitationUsers).LockInvitation(ctx, tx, u.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return u, tenant
}

// invitationEventHandler returns the subscription handler the composition installs
// for one event name, with the envelope a worker would hand it.
func invitationEventHandler(t *testing.T, f invitationSQLFixture, name string) func(context.Context, db.Tx[db.Tenant], events.Event) error {
	t.Helper()
	for _, sub := range internal.InvitationSubscriptions(f.service) {
		if sub.Name == name {
			return sub.Handler
		}
	}
	t.Fatalf("the composition installs no handler for %s", name)
	return nil
}

// invitationStateAfter reads the recorded revision and how many replacement facts
// this tenant's outbox holds for one person.
func invitationStateAfter(t *testing.T, f invitationSQLFixture, id uuid.UUID, run func(context.Context, db.Tx[db.Tenant]) error) (int64, int64) {
	t.Helper()
	var revision, resent int64
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := run(ctx, tx); err != nil {
			return err
		}
		if err := tx.DB().Table("users").Where("id = ?", id).Pluck("invitation_revision", &revision).Error; err != nil {
			return err
		}
		return tx.DB().Table("platformkit_outbox").
			Where("name = ? AND payload->>'userId' = ?", user.EventInvitationResent, id.String()).
			Count(&resent).Error
	}); err != nil {
		t.Fatal(err)
	}
	return revision, resent
}

// TestReplayedInvitationOfferChangesNothing replays one delivery of the event that
// asks for an adopted invitation's mail. A replayed event id is the same work asked
// for again, so the second delivery records no new transition, moves no revision and
// publishes no second fact about it.
func TestReplayedInvitationOfferChangesNothing(t *testing.T) {
	f := invitationSQL(t)
	u, tenant := invitationAdoptedPerson(t, f)
	handler := invitationEventHandler(t, f, user.EventInvited)
	body, err := json.Marshal(user.Invited{UserID: u.ID, Email: u.Email, Status: user.StatusInvited, At: f.clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	ev := events.Event{ID: uuid.New(), Name: user.EventInvited, TenantID: tenant, Payload: body, At: f.clock.Now()}
	deliver := func() (int64, int64) {
		return invitationStateAfter(t, f, u.ID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return handler(ctx, tx, ev)
		})
	}
	firstRevision, firstResent := deliver()
	secondRevision, secondResent := deliver()
	if secondRevision != firstRevision || secondResent != firstResent {
		t.Fatalf("replaying event %s moved the invitation from revision %d with %d published replacement(s) to revision %d with %d: the same delivery is not the same work twice",
			ev.ID, firstRevision, firstResent, secondRevision, secondResent)
	}
}

// TestInvitationOfferDoesNotPublishItsOwnEvent walks what the module hands itself:
// the replacement fact an offer publishes is delivered back to the same handler,
// which is how a retried or replayed worker arrives. One ask for mail is one
// replacement fact, so the walk ends at the first one and no further event appears.
func TestInvitationOfferDoesNotPublishItsOwnEvent(t *testing.T) {
	f := invitationSQL(t)
	u, tenant := invitationAdoptedPerson(t, f)
	invited := invitationEventHandler(t, f, user.EventInvited)
	resent := invitationEventHandler(t, f, user.EventInvitationResent)
	body, err := json.Marshal(user.Invited{UserID: u.ID, Email: u.Email, Status: user.StatusInvited, At: f.clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	ev := events.Event{ID: uuid.New(), Name: user.EventInvited, TenantID: tenant, Payload: body, At: f.clock.Now()}
	invitationStateAfter(t, f, u.ID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return invited(ctx, tx, ev)
	})
	for range 4 {
		var next events.Event
		if err := f.Step(func(_ context.Context, tx db.Tx[db.Tenant]) error {
			var raw []string
			if err := tx.DB().Table("platformkit_outbox").
				Where("name = ? AND payload->>'userId' = ?", user.EventInvitationResent, u.ID.String()).
				Order("created_at DESC, id DESC").Limit(1).Pluck("payload::text", &raw).Error; err != nil {
				return err
			}
			if len(raw) == 0 {
				return nil
			}
			next = events.Event{ID: uuid.New(), Name: user.EventInvitationResent, TenantID: tenant, Payload: json.RawMessage(raw[0])}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if len(next.Payload) == 0 {
			break
		}
		invitationStateAfter(t, f, u.ID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return resent(ctx, tx, next)
		})
	}
	_, published := invitationStateAfter(t, f, u.ID, func(context.Context, db.Tx[db.Tenant]) error { return nil })
	if published > 1 {
		var payloads []string
		if err := f.Step(func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Table("platformkit_outbox").
				Where("name = ? AND payload->>'userId' = ?", user.EventInvitationResent, u.ID.String()).
				Order("created_at, id").Pluck("payload::text", &payloads).Error
		}); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("one ask for mail became %d replacement facts, each of which is delivered to the handler that asks again: %v", published, payloads)
	}
}
