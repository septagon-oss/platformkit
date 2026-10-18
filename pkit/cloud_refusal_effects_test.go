package pkit_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestIncompleteCloudBuildLeavesTheDatabaseEmpty(t *testing.T) {
	for _, env := range []pkit.Environment{pkit.Staging, pkit.Production} {
		t.Run(string(env), func(t *testing.T) {
			cfg := onOneDatabase(t)
			cfg.Cloud.Profile = config.ProfileAWS
			a := pkit.NewApp("collect").Use(doors, desk)
			runtime, err := a.Build(t.Context(), pkit.Deployment{Environment: env, Config: cfg})
			if runtime != nil {
				t.Cleanup(func() { _ = runtime.Close() })
				t.Error("refused cloud build returned a runtime")
			}
			if err == nil {
				t.Fatal("incomplete cloud deployment built")
			}
			for _, key := range []string{
				"files.s3.endpoint", "files.s3.bucket", "files.s3.region", "files.s3.access_key",
				"files.s3.secret_key", "cache.url", "cache.app", "mail.host", "mail.from", "telemetry.otlp_endpoint",
			} {
				if !strings.Contains(err.Error(), "needs "+key+" in "+string(env)) {
					t.Errorf("refusal omits %s: %v", key, err)
				}
			}
			if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
				t.Errorf("refused build created %d tables, including possible event state", got)
			}
		})
	}
}
