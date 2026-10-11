package main

// The operator's credential is one story and `--as` is another: the first proves
// who is standing at the terminal, the second names whose roles authorise every
// write the run makes. A deactivated person keeps their hash, their roles and
// their address, so a run that read the roles off the row without asking the
// status would authorise its writes through somebody the tenant has switched off
// — the same person whose session kit/httpx refuses. The operator's own version
// of this case is TestSeedCommandRefusesADeactivatedOperator; this is the half
// that case cannot reach, because it proves the operator and then asks for the
// administrator as the actor.
//
// The tenant here is created without its demo records and holds two provisioned
// administrators: deactivating the only one is the user module's own last-one-away
// refusal, and that refusal has to be out of the way before this case can ask
// what a run does when the person it was pointed at is switched off.

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

func TestASeedRunRefusesToWriteAsADeactivatedPerson(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	const goneEmail, stayingEmail = "gone@gone-actor.localhost", "staying@gone-actor.localhost"
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		created, err := c.tenants.Create(ctx, system, tenantcontracts.NewTenant{
			Slug: "gone-actor", Name: "Gone", Host: "gone-actor.localhost",
		})
		if err != nil {
			return err
		}
		roles, err := rerunOperatorRoles(ctx, c, system)
		if err != nil {
			return err
		}
		if _, err := c.users.Provision(ctx, system, created.ID, stayingEmail, "Staying", "a long credential of my own", roles); err != nil {
			return err
		}
		gone, err := c.users.Provision(ctx, system, created.ID, goneEmail, "Gone", "a long credential of my own", roles)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, created.Tenancy(), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			person, err := c.users.Deactivate(ctx, tx, gone.ID)
			if err != nil {
				return err
			}
			if person.CanSignIn() {
				t.Fatalf("the deactivated person can still sign in (status %q)", person.Status)
			}
			// The person is still there, still holding the roles, and still the
			// address the run would name. What has gone is the right to act.
			if _, err := seedActor(ctx, c.users, tx, goneEmail); err == nil {
				t.Errorf("seedActor named a deactivated person as this run's author; want the run refused")
			} else if !strings.Contains(err.Error(), "active person") {
				t.Errorf("seedActor refused with %q; want it to say the person is not active", err)
			}
			// The other person, active and holding the same roles, is still an
			// actor: this is a rule about a status and not a broken lookup.
			if _, err := seedActor(ctx, c.users, tx, stayingEmail); err != nil {
				t.Errorf("seedActor of the active person = %v; want a principal", err)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
