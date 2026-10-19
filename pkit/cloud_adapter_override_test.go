package pkit_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestExplicitCloudAdapterOverridesNeedOnlyTheirOwnInputs(t *testing.T) {
	for _, profile := range config.Profiles() {
		for _, surface := range []string{"storage", "cache"} {
			t.Run(profile+"/"+surface, func(t *testing.T) {
				overrides := []config.Override{
					config.Set("cloud.profile", profile),
					config.Set("files.store", "s3"),
					config.Set("files.s3.endpoint", "s3.example.com:443"),
					config.Set("files.s3.bucket", "uploads"),
					config.Set("files.s3.region", "eu-west-1"),
					config.Set("files.s3.access_key", "storage-access"),
					config.Set("files.s3.secret_key", "storage-secret"),
					config.Set("cache.adapter", "valkey"),
					config.Set("cache.url", "valkey://cache.example.com:6379"),
					config.Set("cache.app", "collect"),
					config.Set("mail.host", "smtp.example.com"),
					config.Set("mail.from", "noreply@example.com"),
					config.Set("telemetry.otlp_endpoint", "collector.example.com:4317"),
				}
				want := "local-disk"
				if surface == "storage" {
					overrides = append(overrides, config.Set("files.store", "local-disk"))
					for _, key := range []string{"endpoint", "bucket", "region", "access_key", "secret_key"} {
						overrides = append(overrides, config.Set("files.s3."+key, ""))
					}
				} else {
					want = "in-process"
					overrides = append(overrides, config.Set("cache.adapter", "memory"),
						config.Set("cache.url", ""), config.Set("cache.app", ""), config.Set("cache.password", ""))
				}
				cfg, err := config.Load("../config.example.yaml", overrides...)
				if err != nil {
					t.Fatalf("load explicit %s override: %v", surface, err)
				}
				for _, env := range []pkit.Environment{pkit.Staging, pkit.Production} {
					doc, err := cloudApp().Describe(pkit.Deployment{Environment: env, Config: cfg})
					if err != nil {
						t.Errorf("%s: valid %s override still demands the replaced provider: %v", env, surface, err)
						continue
					}
					found := false
					for _, seam := range doc.Cloud.Seams {
						if string(seam.Seam) == surface {
							found = true
							if seam.Provider != want {
								t.Errorf("%s: %s runs %q, want explicit override %q", env, surface, seam.Provider, want)
							}
						}
					}
					if !found {
						t.Errorf("%s: no %s seam in the resolved document", env, surface)
					}
				}
			})
		}
	}
}
