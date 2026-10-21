package internal_test

import (
	"math"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestConcurrentRequestsKeepASaturatedAccountWindow(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.AccountRequests = contracts.AccountRequestBudget{Quota: 2, Window: time.Hour}
	})
	const body = `{"email":"unknown@example.com"}`
	const path = "/api/v1/public/auth/password/forgot"
	addr := from("203.0.113.85")
	if got := call(t, router, http.MethodPost, path, body, addr); got.Code != http.StatusOK {
		t.Fatalf("opening the counter = %d: %s", got.Code, got.Body)
	}
	var opened time.Time
	if err := admin.QueryRowContext(t.Context(), "UPDATE platformkit_limits SET count = $1, window_start = now() - interval '30 minutes' RETURNING window_start", math.MaxInt32-1).Scan(&opened); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var pending sync.WaitGroup
	for range 8 {
		pending.Go(func() {
			<-start
			if got := call(t, router, http.MethodPost, path, body, addr); got.Code != http.StatusTooManyRequests {
				t.Errorf("concurrent request past saturation = %d, want 429: %s", got.Code, got.Body)
			}
		})
	}
	close(start)
	pending.Wait()
	var count int
	var after time.Time
	if err := admin.QueryRowContext(t.Context(), "SELECT count, window_start FROM platformkit_limits").Scan(&count, &after); err != nil {
		t.Fatal(err)
	}
	if count != math.MaxInt32 || !after.Equal(opened) {
		t.Errorf("concurrent refusals changed the saturated window: count=%d, opened=%s, want count=%d, opened=%s", count, after, math.MaxInt32, opened)
	}
}
