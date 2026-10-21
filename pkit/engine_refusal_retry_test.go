package pkit_test

import (
	"strings"
	"testing"

	engine "github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestEnginePoolRefusalLeavesTheApplicationBuildable(t *testing.T) {
	for _, entry := range []string{"Build", "Run"} {
		t.Run(entry, func(t *testing.T) {
			cfg := onOneDatabase(t)
			cfg.Database.MaxOpenConns = new(2)
			cfg.Database.MaxIdleConns = new(3)
			d := buildDeployment(cfg, engine.All)
			a := pkit.NewApp("collect").Use(doors, desk)
			var err error
			if entry == "Build" {
				var runtime *pkit.Runtime
				runtime, err = a.Build(t.Context(), d)
				if runtime != nil {
					defer runtime.Close()
					t.Fatal("invalid pool returned a runtime")
				}
			} else {
				err = a.Run(t.Context(), d, engine.All)
			}
			if err == nil || !strings.Contains(err.Error(), "database.max_idle_conns") {
				t.Fatalf("invalid pool did not reach its refusal: %v", err)
			}
			if n := tablesIn(t, cfg.Database.MigrateURL); n != 0 {
				t.Fatalf("invalid pool left %d tables", n)
			}
			d.Config.Database.MaxIdleConns = new(2)
			runtime, err := a.Build(t.Context(), d)
			if err != nil {
				t.Fatalf("corrected pool could not build the same application: %v", err)
			}
			if runtime == nil {
				t.Fatal("corrected pool returned no runtime")
			}
			defer runtime.Close()
			if n := tablesIn(t, cfg.Database.MigrateURL); n == 0 {
				t.Error("corrected pool did not run the migrations")
			}
		})
	}
}
