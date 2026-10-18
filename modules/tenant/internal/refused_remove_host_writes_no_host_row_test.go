package internal_test

// At the module: the refusal the delivery says it asks
// before it writes, for the one verb where it asks after.
//
// `internal/service.go` states the rule for the whole delivery:
//
//	The refusal of an unauditable verb is asked *before* the command writes
//	(`audience`), not in the publish step … the refusal then depended on the
//	caller's transaction being rolled back, which is a promise about other
//	people's code.
//
// `TestAnInstallationWithNoOperatorTenantWritesNothing` pins that for `Create`, by
// committing the transaction the error came out of and asserting the database holds
// nothing. `RemoveHost` is the one command that does not hold the line: it runs
// `DELETE FROM tenant_hosts` and only then asks `audience`. Commit the transaction
// that the refusal came out of — the same move the delivery's own case makes — and
// the refused verb has removed a name the tenant still answers at.
//
// The state that makes the refusal happen is the one the delivery's own case names:
// an installation whose operator tenant is gone. Nothing here depends on what the
// broken answer prints: the refusal is asserted by its own error value, and the
// write it should not have made is asserted as a row count on the table.

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

func TestARefusedRemoveHostWritesNoHostRow(t *testing.T) {
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
		if _, err := svc.AddHost(ctx, tx, customer, "www.acme.example.com", false); err != nil {
			return err
		}
		// The installation the delivery's own case names: no operator tenant left.
		return tx.DB().Exec("UPDATE tenants SET operator = false").Error
	}); err != nil {
		t.Fatalf("install an installation, then take its operator tenant away: %v", err)
	}

	err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		_, err := svc.RemoveHost(ctx, tx, customer, "www.acme.example.com")
		if !errors.Is(err, contracts.ErrNoOperatorTenant) {
			t.Errorf("RemoveHost with no operator tenant = %v, want ErrNoOperatorTenant", err)
		}
		// The refusal's own transaction commits, exactly as in the case this one
		// copies: a write the command made before it refused is a write that
		// survives, and the module's rule says there is no such write.
		return nil
	})
	if err != nil {
		t.Fatalf("the refused removal's transaction: %v", err)
	}

	var hosts int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM tenant_hosts WHERE tenant_id = $1`, customer).Scan(&hosts); err != nil {
		t.Fatalf("count the tenant's hosts: %v", err)
	}
	if hosts != 2 {
		t.Errorf("a lifecycle verb that refused wrote the tenant's host list: %d rows remain, want the 2 it had. "+
			"`RemoveHost` deletes the row before it asks `audience`, so the refusal depends on the caller rolling "+
			"back — the promise `audience` exists to avoid making.", hosts)
	}
}
