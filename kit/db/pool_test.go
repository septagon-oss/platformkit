package db_test

import (
	"context"
	"net/url"
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
	pool := db.Pool{MaxOpenConns: 1}
	conn, err := db.OpenWithPool(t.Context(), appURL, pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if stats := conn.Stats(); stats.MaxOpenConnections != 1 || stats.Idle != 0 || stats.InUse != 0 {
		t.Fatalf("custom pool stats = %+v", stats)
	}
	if bad, err := db.OpenWithPool(t.Context(), adminURL, pool); err == nil {
		_ = bad.Close()
		t.Fatal("custom pool accepted an unrestricted role")
	}
}

// The two probe roles, named so nothing else in this database can be them.
const (
	probeLogin  = "pkit_open_probe"
	probeBypass = "pkit_open_probe_bypass"
)

// TestOpenRefusesARoleThatCanSetRolePastRowLevelSecurity.
//
// BYPASSRLS is not inherited, so a role may hold it harmlessly — until the
// application's own role is made a member of it, which one GRANT does. RLS
// attributes are not what Open looks at either: it used to read the connected
// role's own flags and find them clean, while `SET ROLE` inside a transaction
// reached the attribute one statement later and read every tenant's rows past
// every policy in migrations/, FORCE ROW LEVEL SECURITY included. So the question
// is not what this role may do. It is what this role may become.
//
// The probe roles are created rather than granted to platformkit_app, because a
// live membership on the role every other package in this run connects as would
// be a flake handed to the rest of the suite.
func TestOpenRefusesARoleThatCanSetRolePastRowLevelSecurity(t *testing.T) {
	adminURL, appURL := dbtest.URLs(t)
	admin := dbtest.Open(t, adminURL)
	ctx := t.Context()

	for _, statement := range []string{
		"REVOKE " + probeBypass + " FROM " + probeLogin,
		"DROP ROLE IF EXISTS " + probeLogin,
		"DROP ROLE IF EXISTS " + probeBypass,
	} {
		_, _ = admin.ExecContext(ctx, statement) // nothing exists on the first run, which is fine
	}
	for _, statement := range []string{
		"CREATE ROLE " + probeBypass + " NOINHERIT BYPASSRLS",
		"CREATE ROLE " + probeLogin + " LOGIN PASSWORD 'platformkit' NOSUPERUSER NOBYPASSRLS",
		"GRANT " + probeBypass + " TO " + probeLogin,
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, statement := range []string{
			"REVOKE " + probeBypass + " FROM " + probeLogin,
			"DROP ROLE IF EXISTS " + probeLogin,
			"DROP ROLE IF EXISTS " + probeBypass,
		} {
			if _, err := admin.ExecContext(cleanup, statement); err != nil {
				t.Errorf("cleanup %q: %v", statement, err)
			}
		}
	})

	probeURL, err := url.Parse(appURL)
	if err != nil {
		t.Fatal(err)
	}
	probeURL.User = url.UserPassword(probeLogin, "platformkit")

	if _, err := db.OpenWithPool(ctx, probeURL.String(), db.Pool{MaxOpenConns: 1}); err == nil {
		t.Error("Open accepted a role that can SET ROLE to one with BYPASSRLS")
	} else if !strings.Contains(err.Error(), probeBypass) {
		t.Errorf("Open refused without naming the reachable role: %v", err)
	}

	// And it was the membership that decided it, not the login role's own
	// attributes: revoke the grant and the same connection is acceptable.
	if _, err := admin.ExecContext(ctx, "REVOKE "+probeBypass+" FROM "+probeLogin); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	conn, err := db.OpenWithPool(ctx, probeURL.String(), db.Pool{MaxOpenConns: 1})
	if err != nil {
		t.Errorf("Open refused the same role with no grant behind it: %v", err)
		return
	}
	conn.Close()
}
