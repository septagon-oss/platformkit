package events_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// A real uncommitted claim must not block a ledger move in another package run.
// The child uses this package's TestMain and existing tenant-isolation case.
func TestALedgerClaimInOneTestRunDoesNotRefuseAnotherRunsMove(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: tenantID(t, conn, "held-shop", "collect"), Slug: "held-shop"}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	err = db.Run(tenancy.WithTenant(ctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Exec(`INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES (?, ?, ?)`,
			uuid.New(), appname.Durable("", ledgerModule, ledgerEvent), tenant.ID).Error; err != nil {
			return err
		}
		const childTest = "TestAnotherTenantCannotReadOrRewriteAMovedLedger"
		child := exec.CommandContext(ctx, executable, "-test.run=^"+childTest+"$", "-test.v", "-test.count=1", "-test.timeout=4m")
		output, err := child.CombinedOutput()
		if err != nil {
			return fmt.Errorf("another run's ledger move while this claim is open: %w\n%s", err, output)
		}
		if !strings.Contains(string(output), "--- PASS: "+childTest) {
			return fmt.Errorf("the child did not complete its tenant-isolation case:\n%s", output)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
