package internal_test

import (
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestAnExpiredSaturatedAccountWindowRestoresItsConfiguredHeaders(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.AccountRequests = contracts.AccountRequestBudget{Quota: 2, Window: time.Hour}
	})
	got := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot",
		`{"email":"unknown@acme.localhost"}`, from("203.0.113.40"))
	if got.Code != http.StatusOK {
		t.Fatalf("opening the counter = %d: %s", got.Code, got.Body)
	}
	changed, err := admin.ExecContext(t.Context(),
		"UPDATE platformkit_limits SET count = $1, window_start = now() - interval '61 minutes'", math.MaxInt32)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := changed.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("expiring the saturated counter: rows=%d, error=%v", n, err)
	}
	askUntilRefused(t, router, 2, time.Hour)
}
