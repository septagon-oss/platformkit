package auth_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

type refusedSecondRepairWrite struct {
	contracts.Service
	writes       int
	firstChanged bool
	err          error
}

func (s *refusedSecondRepairWrite) SetRoleChanged(ctx context.Context, tx db.Tx[db.Tenant], name string,
	permissions []string, declared []tenancy.Grant,
) (*contracts.Role, bool, error) {
	s.writes++
	if s.writes == 2 {
		return nil, false, s.err
	}
	role, changed, err := s.Service.SetRoleChanged(ctx, tx, name, permissions, declared)
	s.firstChanged = changed && err == nil
	return role, changed, err
}

func TestARefusedRepairRollsBackEarlierRoleChangesAndEvents(t *testing.T) {
	adminDB, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.New(auth.Deps{})
	tenant := ovSeed(t, conn, svc)
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return auth.SeedRoles(ctx, tx, tenant, twoRolesWere, twoRolesDefaults)
	}); err != nil {
		t.Fatal(err)
	}
	before := map[string][]string{}
	for _, name := range []string{"finance", "intern"} {
		before[name] = twoRolesGrants(t, conn, svc, tenant, name)
		if !slices.Contains(before[name], "billing:catalog") {
			t.Fatalf("%s has no dead grant to repair: %v", name, before[name])
		}
	}
	refused := errors.New("role writer unavailable")
	writer := &refusedSecondRepairWrite{Service: svc, err: refused}
	var report map[string][]string
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var err error
			report, err = auth.RepairSeededRoles(ctx, tx, writer, twoRolesNow, twoRolesDefaults, true)
			return err
		})
	if !errors.Is(err, refused) || report != nil {
		t.Fatalf("refused repair returned report %v, error %v", report, err)
	}
	if writer.writes != 2 || !writer.firstChanged {
		t.Fatalf("reached %d writes, first changed=%v, want a successful first write and a refused second", writer.writes, writer.firstChanged)
	}
	for name, want := range before {
		if got := twoRolesGrants(t, conn, svc, tenant, name); !slices.Equal(got, want) {
			t.Errorf("refused repair changed %s from %v to %v", name, want, got)
		}
	}
	if events := twoRolesRoleSet(t, adminDB, tenant); len(events) != 0 {
		t.Errorf("refused repair committed events: %v", events)
	}
}
