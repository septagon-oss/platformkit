package internal_test

// Review round 2 of T-0115, at the module: the rule review round 1 filed as finding 2 is
// still broken one verb over.
//
// Finding 2 was "`RemoveHost` deletes the row and only then asks `audience`, so the refusal
// of an unauditable verb writes." The cure (`a12e6c7`) moved that one call above that one
// DELETE and put a sentence in its place:
//
//	// Asked before the row is deleted, like every other command asks it …
//
// `AddHost` is the command that sentence is about and it does not hold: `attach` — the
// INSERT of the new `tenant_hosts` row — runs first, and `audience` is asked after it
// (modules/tenant/internal/service.go, the eight lines between `s.lock` and `s.record`).
// So the same move that made round 1's case pass — commit the transaction the refusal came
// out of, which is exactly what `TestAnInstallationWithNoOperatorTenantWritesNothing` does
// for `Create` — leaves a hostname attached to the tenant by a verb that refused. The state
// that makes the refusal fire is the state round 1's case names and this one copies: an
// installation with no operator tenant.
//
// The case reaches its assertion through what the fixed behaviour prints: the refusal is
// the delivery's own error value, `contracts.ErrNoOperatorTenant`, which the correct code
// answers too, and the write it must not have made is a count on `tenant_hosts`. Nothing
// here reads a message. It passes as soon as `audience` is asked before `attach`, the way
// `Create`, `Suspend`, `Rename`, `Reactivate`, `RemoveHost` and `Delete` all ask it, and no
// other assertion moves.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/trace"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestARefusedAddHostWritesNoHostRow(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	svc := internal.NewService(nil, nil, "")
	ctx := trace.With(t.Context(), trace.New())

	var customer uuid.UUID
	if err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
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
		// The installation the delivery's own case names: no operator tenant left.
		return tx.DB().Exec("UPDATE tenants SET operator = false").Error
	}); err != nil {
		t.Fatalf("install an installation, then take its operator tenant away: %v", err)
	}

	// The refused verb, in a transaction that commits: the caller does nothing the module
	// can rely on, which is the whole reason `audience` is asked before the write.
	if err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		_, err := svc.AddHost(ctx, tx, customer, "www.acme.example.com", false)
		if !errors.Is(err, contracts.ErrNoOperatorTenant) {
			t.Errorf("AddHost with no operator tenant = %v, want ErrNoOperatorTenant", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("the refused add-host's transaction: %v", err)
	}

	var hosts int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM tenant_hosts WHERE tenant_id = $1`, customer).Scan(&hosts); err != nil {
		t.Fatalf("count the tenant's hosts: %v", err)
	}
	if hosts != 1 {
		t.Errorf("a lifecycle verb that refused wrote the tenant's host list: %d rows remain, want the 1 it had. "+
			"`AddHost` inserts the row before it asks `audience`, so the refusal depends on the caller rolling "+
			"back — the promise `audience` exists not to make, and the sentence on RemoveHost's own call "+
			"(\"like every other command asks it\") is not true of this one.", hosts)
	}
}
