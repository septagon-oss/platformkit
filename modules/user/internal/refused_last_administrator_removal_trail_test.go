package internal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

// TestARefusedRemovalOfTheLastAdministratorIsOnTheTrail: the generated delete route's
// hook is the one place this module records a refusal it cannot write in the caller's
// transaction, because the refusal is the case where nothing else survives. It asks
// about the ordinary state a person reaches in an application that runs the invitation
// lifecycle — an accepted member who holds a role that administers the tenant and is
// the only one who can act — and it asserts both halves of the promise: the removal is
// refused, and the attempt is on the trail with the attempt named. The record is written
// outside the refused transaction, so the trail is read back in a transaction of its own.
func TestARefusedRemovalOfTheLastAdministratorIsOnTheTrail(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	svc := newService()

	at := time.Now().UTC().Truncate(time.Microsecond)
	holderID, managerID := uuid.New(), uuid.New()
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		holder := &contracts.User{Email: "holder@example.com", DisplayName: "Holder",
			Status: contracts.StatusActive, Roles: contracts.Roles{usertest.Administering}}
		holder.Invitation = contracts.InvitationHistory{
			Phase: contracts.InvitationAccepted, Revision: 1, Generation: 1,
			Recipient:   contracts.CanonicalEmail(holder.Email),
			RequestedAt: new(at), ExpiresAt: new(at.Add(time.Hour)),
			AcceptedAt: new(at), AcceptedVia: new(contracts.InvitationDirectPassword),
			DeliveryID: new(uuid.New()),
		}
		holder.ID = holderID
		manager := &contracts.User{Email: "manager@example.com", DisplayName: "Manager",
			Status: contracts.StatusActive, Roles: contracts.Roles{"people"}}
		manager.ID = managerID
		if err := crud.Create(ctx, tx, holder); err != nil {
			return err
		}
		return crud.Create(ctx, tx, manager)
	})
	if err != nil {
		t.Fatalf("seed the tenant: %v", err)
	}

	// The delete route's own door: the row read and locked, the hook given the row as it
	// was read, and the request's pool connection on the context, as kit/httpx puts it.
	err = db.Run(tenancy.WithPrincipal(httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn),
		tenancy.Principal{UserID: managerID}), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := crud.GetForUpdate[*contracts.User](tx, holderID)
		if err != nil {
			return err
		}
		if err := svc.RefuseLastAdministrator(ctx, tx, u); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("removing the last person who can administer the tenant = %v, want it refused", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("refuse the removal: %v", err)
	}

	// The refusal is a returned error and the caller's transaction rolls back with it, so
	// the trail row is the only thing that survives — which is the whole reason it is
	// written outside. Read it where a person reading the trail afterwards would.
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var names []string
		if err := tx.DB().Table("platformkit_outbox").Pluck("name", &names).Error; err != nil {
			return err
		}
		for _, name := range names {
			if name == contracts.EventAdministrationRefused {
				return nil
			}
		}
		t.Errorf("the refused removal left %v on the outbox, want %s among them: the attempt to take the tenant's last administrator away is the one write this module promises to record", names, contracts.EventAdministrationRefused)
		return nil
	})
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
}
