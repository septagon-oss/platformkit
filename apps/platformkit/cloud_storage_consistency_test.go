package main

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestTheCloudStorageDescriptionMatchesTheComposedAdapter(t *testing.T) {
	for _, source := range []string{"loaded", "constructed"} {
		t.Run(source, func(t *testing.T) {
			cfg, err := config.Load("../../config.example.yaml",
				config.Set("cloud.profile", "aws"),
				config.Set("files.s3.endpoint", "s3.example.com:443"),
				config.Set("files.s3.bucket", "uploads"),
				config.Set("files.s3.region", "eu-west-1"),
				config.Set("files.s3.access_key", "storage-access"),
				config.Set("files.s3.secret_key", "storage-secret"),
				config.Set("cache.url", "valkey://cache.example.com:6379"),
				config.Set("cache.app", "platformkit"),
				config.Set("mail.host", "smtp.example.com"),
				config.Set("mail.from", "noreply@example.com"),
				config.Set("telemetry.otlp_endpoint", "collector.example.com:4317"))
			if err != nil {
				t.Fatal(err)
			}
			if source == "constructed" {
				cfg.Files.Store, cfg.Cache.Adapter, cfg.NATS.Transport = "", "", ""
			}
			ref := composeReference(cfg, pkit.Production)
			doc, err := ref.app.Describe(ref.once)
			if err != nil {
				t.Fatalf("describe the complete deployment: %v", err)
			}
			storage, ok := pkit.Value[filecontracts.Storage](ref.planned)
			if !ok {
				t.Fatal("composition provides no storage")
			}
			_, signs := storage.(filecontracts.Signer)
			for _, seam := range doc.Cloud.Seams {
				if seam.Seam == pkit.Storage {
					if (seam.Provider == "s3") != signs {
						t.Errorf("Describe reports %q; composed storage is %T (Signer=%v)", seam.Provider, storage, signs)
					}
					return
				}
			}
			t.Fatal("resolved composition has no storage seam")
		})
	}
}
