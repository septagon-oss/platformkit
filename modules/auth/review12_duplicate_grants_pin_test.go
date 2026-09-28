package auth_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// TestTwoRolesOfOneTenantMayHoldTheSameGrantList pins the domain fact the newest
// case in this branch stands on, which nothing else states. That case refuses its
// repair at COMMIT by adding UNIQUE (tenant_id, permissions) DEFERRABLE, and it is
// a probe rather than a rule only because two roles holding one list is a state the
// product allows. It allows it today, through the ordinary roles write, with nothing
// invented: empty the application's own initial role and it holds the list the
// built-in member was seeded holding, nothing. A migration that turned the probe into
// a rule would refuse that legal write — emptying a role is one the administration
// floor exists to permit — and this is the case that says so, naming both rows. The
// probe's constraint stays a temporary one in that test's own schema, as dbtest gives
// each test one, and this case is why. Helpers and doubles are review 2's, not new
// ones (decision 0022).
func TestTwoRolesOfOneTenantMayHoldTheSameGrantList(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	acme := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	seedForRepair(t, conn, acme)
	handEdit(t, conn, svc, acme, "finance", nil)
	if member, finance := roleGrants(t, conn, svc, acme, contracts.RoleMember), roleGrants(t, conn, svc, acme, "finance"); len(member) != 0 || len(finance) != 0 {
		t.Fatalf("the seeded member holds %v and the emptied initial role holds %v", member, finance)
	}
	handEdit(t, conn, svc, acme, "finance", []string{"role:manage"})
	handEdit(t, conn, svc, acme, "reader", []string{"role:manage"})
	if finance, reader := roleGrants(t, conn, svc, acme, "finance"), roleGrants(t, conn, svc, acme, "reader"); !slices.Equal(finance, reader) || len(reader) != 1 {
		t.Fatalf("finance holds %v and reader holds %v: two roles of one tenant granting the same thing is domain state, so the UNIQUE over (tenant_id, permissions) that repair_roles_listing_test.go adds at COMMIT is a probe in that test's own schema and not something a migration may say", finance, reader)
	}
}
