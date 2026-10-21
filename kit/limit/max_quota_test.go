package limit_test

import (
	"math"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The largest limit is admitted to its last event and refused past it for as long
// as the traffic lasts: the row stops at the integer maximum, so the event after a
// refusal is another refusal rather than an overflow a caller fails open on. The
// row is advanced by hand rather than by two billion events.
func TestTheLargestQuotaKeepsRefusingOnceTheCountReachesItsCeiling(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	l := limit.Postgres(httpx.ConnFrom)
	if _, _, _, err := l.Reserve(ctx, "ada", limit.MaxQuota, window); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if _, err := admin.ExecContext(t.Context(), "UPDATE platformkit_limits SET count = $1", limit.MaxQuota-1); err != nil {
		t.Fatalf("advance the row: %v", err)
	}
	if ok, remaining, _, err := l.Reserve(ctx, "ada", limit.MaxQuota, window); err != nil || !ok || remaining != 0 {
		t.Fatalf("the limit-th event = %v, %d left, %v; want allowed with none left", ok, remaining, err)
	}
	for i := range 3 {
		if ok, remaining, retry, err := l.Reserve(ctx, "ada", limit.MaxQuota, window); err != nil || ok || remaining != 0 || retry <= 0 {
			t.Errorf("event %d past the limit = %v, %d left, %s, %v; want refused inside the window, no error", i+1, ok, remaining, retry, err)
		}
	}
	var count int64
	if err := admin.QueryRowContext(t.Context(), "SELECT count FROM platformkit_limits").Scan(&count); err != nil {
		t.Fatalf("read the row: %v", err)
	}
	if count != math.MaxInt32 {
		t.Errorf("the row counts %d, want it held at %d", count, math.MaxInt32)
	}
}
