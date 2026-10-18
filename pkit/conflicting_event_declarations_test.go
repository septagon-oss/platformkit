package pkit_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestConflictingEventDeclarationsRefuseBeforeEffects(t *testing.T) {
	cfg := onOneDatabase(t)
	ledger := pkit.NewModule("ledger", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "ledger", Declared: []events.Declared{
			events.Declare[postedNumber]("ledger.posted"),
			events.Declare[postedText]("ledger.posted"),
		}}, nil
	})
	runtime, err := pkit.NewApp("collect").Use(doors, desk, ledger).
		Build(t.Context(), buildDeployment(cfg, app.All))
	if runtime != nil {
		defer runtime.Close()
	}
	if err == nil {
		t.Error("Build accepted incompatible declarations of ledger.posted")
	} else if !strings.Contains(err.Error(), "ledger.posted") {
		t.Errorf("the refusal must identify the conflicting event: %v", err)
	}
	if runtime != nil {
		t.Error("a refused composition must return no runtime")
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("the conflicting composition migrated %d tables; want no effects", got)
	}
}
