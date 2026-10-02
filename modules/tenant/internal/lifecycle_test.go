package internal_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

// TestALifecycleCommandWritesTwoRowsWithOneTraceAndTwoScopes is the pillar's
// auditable and traceable lines as one database fact. A suspension writes the verb
// event in the customer's scope and `tenant.lifecycle_recorded` in the installation's,
// in the transaction that wrote the column; both rows carry the request's W3C trace id.
//
// The scope is the half that cannot be faked: which tenant an outbox row *names* is
// what decides whose trail the audit module's subscription writes into
// (kit/events.Consume opens the handler's transaction in that tenant), so a row
// written with the wrong one would land in a stranger's history and every test that
// reads the trail back would still be green. The last assertion is the one that fails.
func TestALifecycleCommandWritesTwoRowsWithOneTraceAndTwoScopes(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	svc := internal.NewService(nil, nil)
	var operator, customer uuid.UUID
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		installed, err := internal.Bootstrap(ctx, tx, svc, contracts.NewTenant{
			Slug: "installation", Name: "This installation", Host: "ops.example.com",
		})
		if err != nil {
			return err
		}
		operator = installed.ID
		created, err := svc.Create(ctx, tx, contracts.NewTenant{
			Slug: "acme", Name: "Acme", Host: "acme.example.com",
		})
		if err != nil {
			return err
		}
		customer = created.ID
		return tx.DB().Exec("DELETE FROM platformkit_outbox").Error
	}); err != nil {
		t.Fatalf("install an installation with one customer: %v", err)
	}

	tr := trace.New()
	err := dbtest.System(trace.With(t.Context(), tr), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if _, err := svc.Suspend(ctx, tx, customer); err != nil {
			return err
		}
		rows := outboxRows(t, tx)
		var subject, mirror *outboxRow
		for i := range rows {
			switch rows[i].Name {
			case contracts.EventSuspended:
				subject = &rows[i]
			case contracts.EventLifecycleRecorded:
				mirror = &rows[i]
			}
		}
		if subject == nil || mirror == nil {
			t.Fatalf("the suspension wrote %v, want the verb and the operator's mirror", namesOf(rows))
		}
		if subject.TenantID != customer {
			t.Errorf("the verb is scoped to %s, want the customer whose state changed", subject.TenantID)
		}
		if mirror.TenantID != operator {
			t.Errorf("the mirror is scoped to %s, want the installation's own tenant %s", mirror.TenantID, operator)
		}
		if subject.TraceParent() != tr.Parent() || mirror.TraceParent() != tr.Parent() {
			t.Errorf("the two rows carry %q and %q, want the request's %q",
				subject.TraceParent(), mirror.TraceParent(), tr.Parent())
		}
		// The customer's own transaction cannot read the installation's row. This
		// is the second tenant's data being unreachable, which is the sentence the
		// boundary has to answer for: the mirror is the operator's, and a tenant
		// that could see it would see which verb was used on it from the wrong side
		// — and a tenant that could see a *second* tenant's row would be the leak.
		return nil
	})
	if err != nil {
		t.Fatalf("suspend under one trace: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: customer}), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var ids []uuid.UUID
			if err := tx.DB().Table("platformkit_outbox").
				Where("name = ?", contracts.EventLifecycleRecorded).Pluck("tenant_id", &ids).Error; err != nil {
				return err
			}
			if len(ids) != 0 {
				t.Errorf("the customer's transaction reads the operator's mirror rows %v, want none", ids)
			}
			var own []uuid.UUID
			if err := tx.DB().Table("platformkit_outbox").Pluck("tenant_id", &own).Error; err != nil {
				return err
			}
			for _, id := range own {
				if id != customer {
					t.Errorf("the customer's transaction reads an outbox row scoped to %s", id)
				}
			}
			return nil
		})
	if err != nil {
		t.Fatalf("read the outbox as the customer: %v", err)
	}

	// And the two rows are really two rows in two tenants, read back through the
	// owner's own connection rather than through the transaction that wrote them.
	var scopes []uuid.UUID
	rows, err := admin.QueryContext(t.Context(),
		`SELECT DISTINCT tenant_id FROM platformkit_outbox WHERE name = ANY($1)`,
		[]string{contracts.EventSuspended, contracts.EventLifecycleRecorded})
	if err != nil {
		t.Fatalf("read the outbox as the owner: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		scopes = append(scopes, id)
	}
	if want := []uuid.UUID{customer, operator}; !(slices.Contains(scopes, want[0]) && slices.Contains(scopes, want[1]) && len(scopes) == 2) {
		t.Errorf("the two events are scoped to %v, want exactly the customer and the installation", scopes)
	}
}

// TestTwoLifecycleCommandsOnOneRowSettleOnce is the race the row lock exists for. One
// transaction suspends a tenant and stays open; a second suspends the same tenant
// meanwhile. Under READ COMMITTED — which is what kit/db gets, having asked Postgres
// for nothing — the second one blocks on the FOR UPDATE the first took, re-reads the
// row as it was committed, and takes the branch that changes nothing and publishes
// nothing. One suspended event is the whole assertion: without the lock both read
// `active`, both wrote, and an operator's retry appeared twice in an audit.
//
// The elapsed time is asserted too, because a second command that did not wait would
// pass the count by accident rather than by locking.
func TestTwoLifecycleCommandsOnOneRowSettleOnce(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	svc := internal.NewService(nil, nil)
	var customer uuid.UUID
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if _, err := internal.Bootstrap(ctx, tx, svc, contracts.NewTenant{
			Slug: "installation", Name: "This installation", Host: "ops.example.com",
		}); err != nil {
			return err
		}
		created, err := svc.Create(ctx, tx, contracts.NewTenant{
			Slug: "acme", Name: "Acme", Host: "acme.example.com",
		})
		if err != nil {
			return err
		}
		customer = created.ID
		return tx.DB().Exec("DELETE FROM platformkit_outbox").Error
	}); err != nil {
		t.Fatalf("install an installation with one customer: %v", err)
	}

	const hold = 400 * time.Millisecond
	first, started, release := make(chan error, 1), make(chan struct{}), make(chan struct{})
	go func() {
		first <- dbtest.System(context.Background(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			if _, err := svc.Suspend(ctx, tx, customer); err != nil {
				return err
			}
			close(started)
			<-release
			return nil
		})
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the first suspension never got as far as holding the row")
	}

	began := time.Now()
	done := make(chan error, 1)
	go func() {
		done <- dbtest.System(context.Background(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			_, err := svc.Suspend(ctx, tx, customer)
			return err
		})
	}()
	time.Sleep(hold)
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the second suspension: %v", err)
		}
		if waited := time.Since(began); waited < hold/2 {
			t.Errorf("the second suspension returned after %v, want it to have waited behind the first", waited)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the second suspension never returned; the row lock outlived its transaction")
	}
	if err := <-first; err != nil {
		t.Fatalf("the first suspension: %v", err)
	}

	err := dbtest.System(context.Background(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var names []string
		if err := tx.DB().Table("platformkit_outbox").Pluck("name", &names).Error; err != nil {
			return err
		}
		suspensions := 0
		for _, n := range names {
			if n == contracts.EventSuspended {
				suspensions++
			}
		}
		if suspensions != 1 {
			t.Errorf("two suspensions of one tenant published %v, want the one event", names)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read what the two commands published: %v", err)
	}
}

// TestAnInstallationWithNoOperatorTenantWritesNothing: the other side of the audit
// rule. With no installation to write its row into, a lifecycle verb refuses, and the
// refusal is the whole transaction — no column, no outbox row, no half of an audit.
// This is the state no request can reach, because the route that would ask for the
// verb is authorized at the operator tenant's own host: what it does name is a
// broken installation, and the honest answer to that is that nothing happened.
func TestAnInstallationWithNoOperatorTenantWritesNothing(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	svc := internal.NewService(nil, nil)

	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		_, err := svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"})
		if !errors.Is(err, contracts.ErrNoOperatorTenant) {
			t.Errorf("Create with no operator tenant = %v, want ErrNoOperatorTenant", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the refused create's transaction: %v", err)
	}

	var tenants, rows int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM tenants`).Scan(&tenants); err != nil {
		t.Fatalf("count the tenants: %v", err)
	}
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&rows); err != nil {
		t.Fatalf("count the outbox: %v", err)
	}
	if tenants != 0 || rows != 0 {
		t.Errorf("the refused create left %d tenants and %d outbox rows behind, want neither", tenants, rows)
	}

	// A verb that has an installation to audit into still works, so the refusal above
	// is the missing row and not the module.
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if _, err := internal.Bootstrap(ctx, tx, svc, contracts.NewTenant{
			Slug: "installation", Name: "This installation", Host: "ops.example.com",
		}); err != nil {
			return err
		}
		created, err := svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"})
		if err != nil {
			return err
		}
		_, err = svc.Delete(ctx, tx, created.ID, contracts.Delete{Confirm: "acme"})
		return err
	}); err != nil {
		t.Fatalf("the same verbs against an installed control plane: %v", err)
	}
	var deleted int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM tenants WHERE slug = 'acme' AND deleted_at IS NOT NULL`).Scan(&deleted); err != nil {
		t.Fatalf("count the retired tenant: %v", err)
	}
	if deleted != 1 {
		t.Errorf("the installed control plane retired %d tenants, want the one it was told to", deleted)
	}
}

// TestARenamedTenantStillAnswersAtItsOwnHost is the shape a rename leaves the rest of
// the control plane in: the slug, the hosts and the languages are where they were, and
// the name the resolution carries is the one a page will show.
func TestARenamedTenantStillAnswersAtItsOwnHost(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	svc := internal.NewService(nil, nil)
	var customer uuid.UUID
	err := dbtest.System(trace.With(t.Context(), trace.New()), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if _, err := internal.Bootstrap(ctx, tx, svc, contracts.NewTenant{
			Slug: "installation", Name: "This installation", Host: "ops.example.com",
		}); err != nil {
			return err
		}
		created, err := svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"})
		if err != nil {
			return err
		}
		customer = created.ID
		renamed, err := svc.Rename(ctx, tx, customer, contracts.Rename{Name: "Acme Industries"})
		if err != nil {
			return err
		}
		if renamed.Slug != "acme" {
			t.Errorf("the rename moved the slug to %q", renamed.Slug)
		}
		resolved, err := svc.ByHost(ctx, tx, "acme.example.com")
		if err != nil {
			return err
		}
		if resolved.Name != "Acme Industries" || resolved.ID != customer {
			t.Errorf("the host resolves to %+v, want the renamed tenant", resolved)
		}
		// And a rename of a name nobody typed differently is silent: the retry of a
		// form that was resubmitted is not a second entry in two trails.
		before := len(outboxRows(t, tx))
		if _, err := svc.Rename(ctx, tx, customer, contracts.Rename{Name: "Acme Industries"}); err != nil {
			return err
		}
		if after := len(outboxRows(t, tx)); after != before {
			t.Errorf("renaming a tenant to the name it has published %d more rows, want none", after-before)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("rename a tenant: %v", err)
	}
}

// TestTheLastHostAndThePrimaryHostAreRefusedInsideTheTransaction is the floor read
// from the database rather than the fake: the host rows are where they were, the
// tenant still resolves, and the refusal is a conflict that names the verb instead.
func TestTheLastHostAndThePrimaryHostAreRefusedInsideTheTransaction(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	svc := internal.NewService(nil, nil)
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if _, err := internal.Bootstrap(ctx, tx, svc, contracts.NewTenant{
			Slug: "installation", Name: "This installation", Host: "ops.example.com",
		}); err != nil {
			return err
		}
		created, err := svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"})
		if err != nil {
			return err
		}
		if _, err := svc.RemoveHost(ctx, tx, created.ID, "acme.example.com"); !errors.Is(err, crud.ErrConflict) {
			t.Errorf("removing a tenant's only host = %v, want ErrConflict", err)
		}
		if _, err := svc.AddHost(ctx, tx, created.ID, "www.acme.example.com", false); err != nil {
			return err
		}
		if _, err := svc.RemoveHost(ctx, tx, created.ID, "acme.example.com"); !errors.Is(err, crud.ErrConflict) {
			t.Errorf("removing the primary host = %v, want ErrConflict", err)
		}
		if _, err := svc.RemoveHost(ctx, tx, created.ID, "www.acme.example.com"); err != nil {
			t.Errorf("removing a name that is not the primary one = %v, want it allowed", err)
		}
		after, err := svc.Get(ctx, tx, created.ID)
		if err != nil {
			return err
		}
		// The refused calls wrote nothing, and the one removal removed that one
		// name: what is left is the host the tenant was created with, still the
		// primary one, in the one order every read uses.
		if want := []string{"acme.example.com"}; !slices.Equal(after.Hosts, want) {
			t.Errorf("the tenant is served at %v after the refusals and one removal, want %v", after.Hosts, want)
		}
		var served []string
		if err := tx.DB().Table("tenant_hosts").Where("tenant_id = ? AND is_primary", created.ID).
			Pluck("host", &served).Error; err != nil {
			return err
		}
		if want := []string{"acme.example.com"}; !slices.Equal(served, want) {
			t.Errorf("the primary host row is %v, want %v: the refusals moved nothing", served, want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("test the floors: %v", err)
	}
}

// outboxRows is the outbox as the three columns the lifecycle cases read.
func outboxRows(t *testing.T, tx db.Tx[db.System]) []outboxRow {
	t.Helper()
	var rows []outboxRow
	if err := tx.DB().Table("platformkit_outbox").Order("created_at, id").Find(&rows).Error; err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	return rows
}

func namesOf(rows []outboxRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}
