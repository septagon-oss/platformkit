package internal_test

// The refused-account-email cases compare installationRows before a call with the same reading
// after, and the claim they are most often read for — that a refused sign-up leaves no account —
// holds only while that reading reaches an account whatever tenant id carries it. The token
// tables and the event outbox have this premise pinned for them; the account row does not. The
// case that counts all four tables asks only that its seeding left something the installation can
// see, and a count taken behind the tenant under test still sees that tenant's own seeding, so a
// reading narrowed to one tenant would leave its guard non-zero and every assertion true while the
// brief's "no account" went unproved. So this asks the premise of the account row itself, on the
// helpers those cases call: a person filed for another tenant moves the installation's count of
// users and not the count of the tenant the refused doors are asked about.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

func TestTheInstallationReadTheRefusedDoorCasesCallReachesAnotherTenantsAccount(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	seed(t, conn, acme)
	seed(t, conn, globex)
	person(t, conn, "ada@acme.localhost")
	personAt(t, conn, globex, "carol@globex.localhost")

	before, behindA := installationRows(t, conn)["users"], countsByTable(t, conn)["users"]
	if before <= behindA {
		t.Errorf("the installation counts %d accounts where acme counts %d: this case needs an account acme may not see, or the comparison would answer for an empty schema", before, behindA)
	}

	personAt(t, conn, globex, "bob@globex.localhost")

	if grew := installationRows(t, conn)["users"] - before; grew != 1 {
		t.Errorf("the installation's count of accounts moved by %d where one tenant filed one person: the reading the refused-door cases compare does not reach another tenant's account, so the zero they assert for a refused sign-up would answer for a policy rather than for the refusal", grew)
	}
	if moved := countsByTable(t, conn)["users"] - behindA; moved != 0 {
		t.Errorf("acme's count of accounts moved by %d on a person only globex has: the tenant reading is not the narrow one the before-and-after comparisons take", moved)
	}
}
