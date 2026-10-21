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

func TestConcurrentRequestsShareOneQuotaAfterASaturatedWindowExpires(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.AccountRequests = contracts.AccountRequestBudget{Quota: 3, Window: time.Hour}
	})
	const path = "/api/v1/public/auth/password/forgot"
	const body = `{"email":"unknown@example.com"}`
	addr := from("203.0.113.86")
	if got := call(t, router, http.MethodPost, path, body, addr); got.Code != http.StatusOK {
		t.Fatalf("opening the counter = %d: %s", got.Code, got.Body)
	}
	var expired time.Time
	if err := admin.QueryRowContext(t.Context(), "UPDATE platformkit_limits SET count = $1, window_start = now() - interval '61 minutes' RETURNING window_start", math.MaxInt32).Scan(&expired); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan int, 8)
	var pending sync.WaitGroup
	for range cap(results) {
		pending.Go(func() {
			<-start
			results <- call(t, router, http.MethodPost, path, body, addr).Code
		})
	}
	close(start)
	pending.Wait()
	close(results)
	statuses := make(map[int]int)
	for status := range results {
		statuses[status]++
	}
	if statuses[http.StatusOK] != 3 || statuses[http.StatusTooManyRequests] != 5 {
		t.Errorf("requests after expiry = %v, want three 200 and five 429", statuses)
	}
	var count int
	var opened time.Time
	if err := admin.QueryRowContext(t.Context(), "SELECT count, window_start FROM platformkit_limits").Scan(&count, &opened); err != nil {
		t.Fatal(err)
	}
	if count != cap(results) || !opened.After(expired) {
		t.Errorf("expired saturation did not become one shared window: count=%d opened=%s expired=%s", count, opened, expired)
	}
}
