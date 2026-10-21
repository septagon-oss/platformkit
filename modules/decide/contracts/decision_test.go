package contracts_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
	"github.com/septagon-oss/platformkit/modules/decide/contracts/decidetest"
)

func TestBallotConformance(t *testing.T) {
	decidetest.RunVoting(t, func(t *testing.T, run func(decidetest.Fixture)) {
		f := decidetest.NewFake(decidetest.At)
		for _, person := range []uuid.UUID{decidetest.Alice, decidetest.Bob, decidetest.Casey, decidetest.Evan} {
			f.Grants.Grant(person, contracts.PermissionRead, contracts.PermissionVote, contracts.PermissionManage)
		}
		ctx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: decidetest.Tenant})
		run(decidetest.Fixture{Ctx: ctx, Service: f, Put: f.Put, Snapshot: f.Snapshot,
			Published: f.Events.Names, SetTime: f.Clock.Set, JoinProof: f.JoinVerified, UndoProof: f.UndoVerified,
			OtherTenant: func(run func(context.Context, db.Tx[db.Tenant])) {
				other := tenancy.WithTenant(ctx, tenancy.Tenant{ID: uuid.New()})
				run(other, db.Tx[db.Tenant]{})
			}})
	})
}
