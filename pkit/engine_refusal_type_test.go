package pkit_test

import (
	"errors"
	"strings"
	"testing"

	engine "github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestEnginePoolRefusalsKeepTheirDocumentedErrorType(t *testing.T) {
	cfg := onOneDatabase(t)
	cfg.Database.MaxOpenConns = new(2)
	cfg.Database.MaxIdleConns = new(3)
	d := buildDeployment(cfg, engine.All)
	for _, entry := range []string{"Build", "Run"} {
		t.Run(entry, func(t *testing.T) {
			a := pkit.NewApp("collect").Use(doors, desk)
			plan, err := a.Plan(d)
			if err != nil || plan == nil {
				t.Fatalf("composition did not reach the engine boundary: plan=%v, error=%v", plan, err)
			}
			if entry == "Build" {
				var runtime *pkit.Runtime
				runtime, err = a.Build(t.Context(), d)
				if runtime != nil {
					defer runtime.Close()
					t.Fatal("invalid engine pool returned a runtime")
				}
			} else {
				err = a.Run(t.Context(), d, engine.All)
			}
			if err == nil || !strings.Contains(err.Error(), "database.max_idle_conns") {
				t.Fatalf("invalid engine pool did not return its refusal: %v", err)
			}
			if _, ok := errors.AsType[*pkit.CompositionError](err); ok {
				t.Errorf("engine refusal became a composition error: %v", err)
			}
			t.Logf("engine refusal: %v", err)
		})
	}
	if n := tablesIn(t, cfg.Database.MigrateURL); n != 0 {
		t.Errorf("invalid engine pool left %d tables", n)
	}
}
