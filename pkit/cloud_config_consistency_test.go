package pkit_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestModulesReadTheCloudProvidersThatDescribeReports(t *testing.T) {
	for _, profile := range []string{config.ProfileAWS, config.ProfileGCP, config.ProfileSelfHosted} {
		t.Run(profile, func(t *testing.T) {
			cfg := complete(profile, "")
			var files config.Files
			var cache config.Cache
			var events config.NATS
			reader := pkit.NewModule("settings", func(w *pkit.Wiring) (module.Module, error) {
				files = pkit.Config(w, func(c config.Config) config.Files { return c.Files })
				cache = pkit.Config(w, func(c config.Config) config.Cache { return c.Cache })
				events = pkit.Config(w, func(c config.Config) config.NATS { return c.NATS })
				return module.Module{Name: "settings"}, nil
			})
			a := pkit.NewApp("collect").Use(reader)
			doc, err := a.Describe(pkit.Deployment{Environment: pkit.Production, Config: cfg})
			if err != nil {
				t.Fatalf("complete profile refused: %v", err)
			}
			if doc.Cloud == nil {
				t.Fatal("resolved composition has no cloud description")
			}
			read := map[pkit.Seam]string{
				pkit.Storage: files.Store, pkit.Cache: cache.Adapter, pkit.Events: events.Transport,
			}
			for _, seam := range doc.Cloud.Seams {
				if got, ok := read[seam.Seam]; ok && got != seam.Provider {
					t.Errorf("%s: module reads %q but Describe reports %q", seam.Seam, got, seam.Provider)
				}
			}
			if cfg.Files.Store != "" || cfg.Cache.Adapter != "" || cfg.NATS.Transport != "" {
				t.Error("resolution mutated the caller's configuration")
			}
		})
	}
}
