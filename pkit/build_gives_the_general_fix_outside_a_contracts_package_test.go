package pkit_test

// Build refuses a missing provider whose contract lives outside any
// …/<module>/contracts package with the same general fix Validate gives, and
// refuses it before the schema is touched.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestBuildGivesTheGeneralFixForAContractOutsideAnyContractsPackage(t *testing.T) {
	door := pkit.NewModule("door", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "door"}, nil
	}, pkit.Needs[httpx.Entitler]())
	cfg := onOneDatabase(t)
	a := pkit.NewApp("collect").Use(door)

	_, err := a.Build(t.Context(), pkit.Deployment{Environment: pkit.Development, Config: cfg})
	says(t, err, "pkit: collect: Use: door needs httpx.Entitler: add a module that provides it")
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("the refused build left %d tables behind", got)
	}
}
