package app

import (
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
)

func TestDatabasePoolDefaultsAndExplicitZero(t *testing.T) {
	if got := databasePool(config.Database{}); got != db.DefaultPool() {
		t.Fatalf("defaults = %+v", got)
	}
	got := databasePool(config.Database{MaxOpenConns: new(3), MaxIdleConns: new(0), ConnMaxLifetime: new(time.Duration(0))})
	if got != (db.Pool{MaxOpenConns: 3}) {
		t.Fatalf("explicit pool = %+v", got)
	}
}

func TestAppPoolLeavesRoomForDetachedWork(t *testing.T) {
	opts := transportOptions("")
	cfg := config.Config{Database: config.Database{MaxOpenConns: new(1), MaxIdleConns: new(0)}, NATS: config.NATS{URL: "nats://localhost:4222"}}
	for _, role := range []Role{Web, Worker, All, ""} {
		opts.Role = role
		if _, err := New(t.Context(), cfg, nil, opts); err == nil || !strings.Contains(err.Error(), "advisory-lock") {
			t.Errorf("role %q = %v", role, err)
		}
	}
	opts.Role = Web
	cfg.Database.MaxOpenConns = new(2)
	if _, err := New(t.Context(), cfg, nil, opts); err != nil {
		t.Fatalf("two-connection web pool: %v", err)
	}
	cfg.Database.ConnMaxLifetime = new(-time.Second)
	if _, err := New(t.Context(), cfg, nil, opts); err == nil || !strings.Contains(err.Error(), "conn_max_lifetime") {
		t.Fatalf("invalid pool = %v", err)
	}
	// This URL cannot dial or migrate: invalid options must fail first.
	cfg.Database.MigrateURL = "invalid"
	if err := Bootstrap(t.Context(), cfg, nil, nil); err == nil || !strings.Contains(err.Error(), "conn_max_lifetime") {
		t.Fatalf("invalid bootstrap pool = %v", err)
	}
}
