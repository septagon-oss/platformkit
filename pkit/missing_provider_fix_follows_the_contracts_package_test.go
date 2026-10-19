package pkit_test

// A missing provider names the module to add only when the contract lives in a
// …/<module>/contracts package; a contract declared anywhere else is answered
// with the general fix, never with a module named after its package.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/collectibles"
)

func TestAMissingProviderNamesAModuleToAddOnlyWhenTheContractsPackageNamesOne(t *testing.T) {
	door := pkit.NewModule("door", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "door"}, nil
	}, pkit.Needs[httpx.Entitler]())

	for _, tc := range []struct {
		name string
		app  *pkit.App
		want string
	}{
		{"a contract in a module's contracts package", pkit.NewApp("collect").Use(collectibles.Module),
			"pkit: collect: Use: collectibles needs cartcontracts.Service: add cart.Module to collect"},
		{"a contract outside any contracts package", pkit.NewApp("collect").Use(door),
			"pkit: collect: Use: door needs httpx.Entitler: add a module that provides it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			says(t, tc.app.Validate(dev), tc.want)
			_, err := tc.app.Explain(dev)
			says(t, err, tc.want)
		})
	}
}
