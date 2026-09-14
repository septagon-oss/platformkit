package config_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/config"
)

func TestPoolConfigurationDistinguishesOmissionFromZero(t *testing.T) {
	data, err := os.ReadFile(example)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, settings string
		present        bool
	}{
		{"omitted", "", false},
		{"explicit", "  max_open_conns: 3\n  max_idle_conns: 0\n  conn_max_lifetime: 0s\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir() + "/config.yaml"
			if err := os.WriteFile(path, []byte(strings.Replace(string(data), "database:\n", "database:\n"+tc.settings, 1)), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			pool := cfg.Database
			if !tc.present {
				if pool.MaxOpenConns != nil || pool.MaxIdleConns != nil || pool.ConnMaxLifetime != nil {
					t.Fatalf("omission became an explicit pool: %+v", pool)
				}
			} else if pool.MaxOpenConns == nil || *pool.MaxOpenConns != 3 || pool.MaxIdleConns == nil || *pool.MaxIdleConns != 0 || pool.ConnMaxLifetime == nil || *pool.ConnMaxLifetime != time.Duration(0) {
				t.Fatalf("explicit zero was lost: %+v", pool)
			}
		})
	}
}
