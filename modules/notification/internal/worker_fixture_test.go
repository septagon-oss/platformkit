package internal_test

import (
	"context"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

// oneTenant answers jobs.TenantLister with the tenants a test wrote rows for. The
// worker walks every tenant it is given, and a fixture that named none would drain
// nothing.
type oneTenant struct{ list []tenancy.Tenant }

func (o oneTenant) List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) {
	return o.list, nil
}

var acmeOnly = oneTenant{[]tenancy.Tenant{acme}}

// drainMail runs the delivery worker's tick once over this fixture's tenants, which
// is the only way a committed intent leaves: the decision and the send are two
// transactions, and a test that ran only the handler tested the decision.
func drainMail(t *testing.T, conn *db.Conn, svc *internal.Service) {
	t.Helper()
	drainMailLeased(t, conn, svc, time.Minute)
}

func drainMailLeased(t *testing.T, conn *db.Conn, svc *internal.Service, lease time.Duration) {
	t.Helper()
	err := internal.Deliver(svc, acmeOnly, time.Second, 10, lease).Run(tenancy.WithTenant(t.Context(), acme), conn)
	if err != nil {
		t.Fatalf("deliver the owed mail: %v", err)
	}
}

var _ jobs.TenantLister = oneTenant{}
