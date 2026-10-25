package internal_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
	"github.com/septagon-oss/platformkit/modules/user/internal"
)

// TestARefusedErasureOfTheLastAdministratorIsOnTheTrail erases the only person who can
// administer the tenant and then reads the trail: the erasure is refused for the reason the
// last-administrator rule gives at every other door, so the attempt it made is on the outbox
// beside theirs, naming the grant the refused write would have taken away.
//
// The record is asked for by tenant and subject alone, so the door may name itself in the
// payload's `attempt` the way its own command does. The connection is already on the context
// the erasure runs with, which is what lets a refusal be written outside the transaction that
// refused it; at a door carrying no request connection nothing could be written, and this
// case would say nothing.
func TestARefusedErasureOfTheLastAdministratorIsOnTheTrail(t *testing.T) {
	conn, ctx, _ := tenantWatched(t)
	svc := newService()
	ada := administratorIn(t, ctx, conn, svc, "ada@acme.example.com")

	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData(svc).Data.Erase(ctx, tx,
			personaldata.Subject{ID: ada, Email: "ada@acme.example.com"})
	})
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("erasing the last administrator = %v, want ErrInvalid", err)
	}

	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		refused := tx.DB().Table("platformkit_outbox").
			Where("name = ? AND payload->>'userId' = ?", contracts.EventAdministrationRefused, ada.String())
		var count int64
		if err := refused.Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			var names []string
			if err := tx.DB().Table("platformkit_outbox").Pluck("name", &names).Error; err != nil {
				return err
			}
			t.Errorf("the refused erasure left %d records of the attempt, want 1; the outbox holds %v", count, names)
			return nil
		}
		var namesGrant int64
		if err := tx.DB().Table("platformkit_outbox").
			Where("name = ? AND payload->>'userId' = ? AND payload->'roles' @> ?::jsonb",
				contracts.EventAdministrationRefused, ada.String(), fmt.Sprintf("[%q]", usertest.Administering)).
			Count(&namesGrant).Error; err != nil {
			return err
		}
		if namesGrant != 1 {
			t.Errorf("the recorded attempt does not name %s as the grant the write would have taken", usertest.Administering)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
}
