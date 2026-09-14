package db_test

import (
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestPoolValidationPrecedesConnecting(t *testing.T) {
	if got := db.DefaultPool(); got != (db.Pool{MaxOpenConns: 16, MaxIdleConns: 4, ConnMaxLifetime: 30 * time.Minute}) {
		t.Fatalf("legacy defaults changed: %+v", got)
	}
	for _, tc := range []struct {
		pool db.Pool
		key  string
	}{
		{db.Pool{}, "max_open_conns"},
		{db.Pool{MaxOpenConns: -1}, "max_open_conns"},
		{db.Pool{MaxOpenConns: 1, MaxIdleConns: 2}, "max_idle_conns"},
		{db.Pool{MaxOpenConns: 1, MaxIdleConns: -1}, "max_idle_conns"},
		{db.Pool{MaxOpenConns: 1, ConnMaxLifetime: -time.Second}, "conn_max_lifetime"},
	} {
		if _, err := db.OpenWithPool(t.Context(), "invalid DSN", tc.pool); err == nil || !strings.Contains(err.Error(), tc.key) {
			t.Errorf("pool %+v = %v", tc.pool, err)
		}
	}
}

func TestOpenAppliesPoolLimitsAndStillRefusesUnrestrictedRoles(t *testing.T) {
	adminURL, appURL := dbtest.URLs(t)
	pool := db.Pool{MaxOpenConns: 2}
	conn, err := db.OpenWithPool(t.Context(), appURL, pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if stats := conn.Stats(); stats.MaxOpenConnections != 2 || stats.Idle != 0 || stats.InUse != 0 {
		t.Fatalf("custom pool stats = %+v", stats)
	}
	if bad, err := db.OpenWithPool(t.Context(), adminURL, pool); err == nil {
		_ = bad.Close()
		t.Fatal("custom pool accepted an unrestricted role")
	}
}
