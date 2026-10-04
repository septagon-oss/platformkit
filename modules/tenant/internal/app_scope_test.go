// Two compositions, one schema, one table of tenants. This is the deployment
// decision 0074 §6 puts on its feet: the tenant is the boundary inside an app and
// says nothing across two of them — both compositions mint uuids the same way and
// name their modules with the same vocabulary — so the control plane has to know
// which app it is answering for before it answers. The column is
// migrations/000043_tenant_app; the reads that scope on it are here.
package internal_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
)

func TestTwoAppsOfOneDatabaseSeeNothingOfEachOther(t *testing.T) {
	_, conn := dbtest.Schema(t)
	acme := internal.NewService(nil, nil, "acme")
	academy := internal.NewService(nil, nil, "academy")

	// Each app's own installation, which modules/tenant's Create asks for before it
	// writes anything: a lifecycle verb mirrors its audit copy into the installation of
	// the app that is writing, and an installation of no app is no app's (that is what
	// migrations/000043's tenants_operator unique on (app) says). So the world gains two
	// tenants, one per app, and the reads below see this app's own installation as the
	// tenant of this app that it is — while academy's is the row an app-blind read would
	// answer with, which is the point of the count assertions.
	installedAs(t, conn, acme, "acme-installation", "acme.ops.example.com")
	installedAs(t, conn, academy, "academy-installation", "academy.ops.example.com")

	mint := func(svc *internal.Service, slug, host string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			created, err := svc.Create(ctx, tx, contracts.NewTenant{Slug: slug, Name: slug, Host: host})
			if err != nil {
				return err
			}
			id = created.ID
			return nil
		}); err != nil {
			t.Fatalf("create %s: %v", slug, err)
		}
		return id
	}
	acmeID := mint(acme, "acme", "acme.example.com")
	academyID := mint(academy, "academy", "academy.example.com")

	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		// Each app reads its own row and finds nothing at the other's id — not an
		// empty tenant, and not a refusal that says the row exists.
		own, err := acme.Get(ctx, tx, acmeID)
		if err != nil {
			return err
		}
		if own.App != "acme" {
			t.Errorf("the create stamped app %q, want the composition that made the row", own.App)
		}
		if _, err := academy.Get(ctx, tx, acmeID); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("the other app's Get of acme's id = %v, want it as absent", err)
		}

		// The list an operator's screen reads is one app's tenants, and the names are
		// the whole of the assertion: academy, its installation and its customer are in
		// the same table and none of them appears here. Acme's own installation does,
		// because it is a tenant of acme like any other — and it is first, because it
		// was created first.
		list, err := acme.List(ctx, tx)
		if err != nil {
			return err
		}
		if len(list) != 2 || list[0].Slug != "acme-installation" || list[1].Slug != "acme" {
			names := make([]string, 0, len(list))
			for _, tn := range list {
				names = append(names, tn.Slug)
			}
			t.Errorf("acme's List returned %v, want only its own tenant", names)
		}

		// Host resolution is the read every request makes, and the one that would
		// serve app academy another app's rows under a host academy does not hold.
		if got, err := acme.ByHost(ctx, tx, "acme.example.com"); err != nil || got.ID != acmeID {
			t.Errorf("acme resolving its own host = %v, %v", got.ID, err)
		}
		if got, err := academy.ByHost(ctx, tx, "acme.example.com"); !errors.Is(err, tenancy.ErrNoSuchHost) {
			t.Errorf("academy resolving acme's host = %v, %v, want no such host", got.ID, err)
		}
		if got, err := academy.ByHost(ctx, tx, "academy.example.com"); err != nil || got.ID != academyID {
			t.Errorf("academy resolving its own host = %v, %v", got.ID, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the two apps' reads: %v", err)
	}

	// One operator per app rather than per database: two installations that share
	// the schema have nothing to do with each other, and each needs its own. Asked of
	// two apps that have none yet — acme and academy installed theirs at the top of
	// this case, and that is what a boot does.
	beacon := internal.NewService(nil, nil, "beacon")
	cedar := internal.NewService(nil, nil, "cedar")
	operate := func(svc *internal.Service, slug string) error {
		return dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			_, err := svc.Create(ctx, tx, contracts.NewTenant{
				Slug: slug, Name: slug, Host: slug + ".example.org", Operator: true})
			return err
		})
	}
	if err := operate(beacon, "beacon-ops"); err != nil {
		t.Errorf("beacon's operator tenant: %v, want one operator per app", err)
	}
	if err := operate(cedar, "cedar-ops"); err != nil {
		t.Errorf("cedar's operator tenant: %v, want one operator per app", err)
	}
	if err := operate(beacon, "beacon-ops-again"); err == nil {
		t.Error("a second operator tenant inside one app was accepted; tenants_operator is unique on (app)")
	} else if !errors.Is(err, crud.ErrConflict) {
		t.Errorf("a second operator tenant of one app = %v, want the conflict the index answers", err)
	}
}

// TestATenantWrittenWithoutAnAppIsNotReadByOne is the floor under a row the service
// did not write. The column's default takes the app the session declares and the
// empty slug when it declares none (migrations/000043), so a raw insert lands on
// the app-less deployment — and an app that names itself must not read it, because
// nothing stamped it as its own.
func TestATenantWrittenWithoutAnAppIsNotReadByOne(t *testing.T) {
	_, conn := dbtest.Schema(t)
	var id uuid.UUID
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if err := tx.DB().Exec(
			`INSERT INTO tenants (slug, name) VALUES ('unlabelled', 'Unlabelled')`).Error; err != nil {
			return err
		}
		var ids []uuid.UUID
		if err := tx.DB().Table("tenants").Where("slug = ?", "unlabelled").Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) != 1 {
			return fmt.Errorf("the raw insert left %d rows, want one", len(ids))
		}
		id = ids[0]
		return nil
	}); err != nil {
		t.Fatalf("a raw tenant row: %v", err)
	}
	scoped := internal.NewService(nil, nil, "acme")
	unscoped := internal.NewService(nil, nil, "")
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if _, err := scoped.Get(ctx, tx, id); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("app acme reading an unlabelled row = %v, want it as absent", err)
		}
		own, err := unscoped.Get(ctx, tx, id)
		if err != nil {
			return err
		}
		if own.App != "" {
			t.Errorf("the row's app = %q, want the empty slug the column's default gives it", own.App)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reading the unlabelled row back: %v", err)
	}
}
