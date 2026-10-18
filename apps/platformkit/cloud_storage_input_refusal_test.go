package main

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestMissingCloudStorageInputsReturnErrorsWithoutPanicking(t *testing.T) {
	for _, profile := range []string{config.ProfileAWS, config.ProfileAzure, config.ProfileLocal} {
		for _, missing := range []string{"endpoint", "bucket"} {
			t.Run(profile+"/"+missing, func(t *testing.T) {
				path, _ := configureFresh(t)
				cfg, err := config.Load(path,
					config.Set("cloud.profile", profile),
					config.Set("files.store", "s3"),
					config.Set("files.s3.endpoint", "s3.example.com:443"),
					config.Set("files.s3.bucket", "uploads"),
					config.Set("files.s3.region", "eu-west-1"),
					config.Set("files.s3.access_key", "storage-access"),
					config.Set("files.s3.secret_key", "storage-secret"),
					config.Set("files.s3."+missing, ""),
					config.Set("cache.adapter", "valkey"),
					config.Set("cache.url", "valkey://cache.example.com:6379"),
					config.Set("cache.app", "platformkit"),
					config.Set("mail.host", "smtp.example.com"),
					config.Set("mail.from", "noreply@example.com"),
					config.Set("telemetry.otlp_endpoint", "collector.example.com:4317"))
				if err != nil {
					t.Fatalf("load storage configuration: %v", err)
				}
				defer func() {
					if panicValue := recover(); panicValue != nil {
						t.Errorf("Build panicked instead of returning the missing %s: %v", missing, panicValue)
					}
					owner := dbtest.Open(t, cfg.Database.MigrateURL)
					var tables int
					if err := owner.QueryRowContext(t.Context(),
						"SELECT count(*) FROM pg_tables WHERE schemaname = current_schema()").Scan(&tables); err != nil {
						t.Fatal(err)
					}
					if tables != 0 {
						t.Errorf("refused Build created %d tables", tables)
					}
				}()
				d := pkit.Deployment{Environment: pkit.Production, Config: cfg, Inputs: deploymentInputs(cfg)}
				runtime, err := sentencesOf(cfg).Build(t.Context(), d)
				if runtime != nil {
					t.Cleanup(func() { _ = runtime.Close() })
					t.Error("refused Build returned a runtime")
				}
				if err == nil {
					t.Fatal("Build accepted an S3 store with a missing input")
				}
				if !strings.Contains(strings.ToLower(err.Error()), missing) {
					t.Errorf("Build refusal does not name the missing %s: %v", missing, err)
				}
			})
		}
	}
}
