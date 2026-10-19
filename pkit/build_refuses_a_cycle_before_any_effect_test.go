package pkit_test

// A cycle is refused by Build with the sentence Validate says, edge reasons
// included, before any module builds and before the first migration: the
// schema Build is pointed at holds no table afterwards.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/cart"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/collectibles"
)

func TestBuildRefusesACycleBeforeAnyEffect(t *testing.T) {
	built := false
	shelf := pkit.NewModule("shelf", func(*pkit.Wiring) (module.Module, error) {
		built = true
		return module.Module{Name: "shelf"}, nil
	}, pkit.Needs[cartcontracts.Service](), pkit.Contributes[cartcontracts.Extension]())

	for _, tc := range []struct {
		name string
		app  func() *pkit.App
		want string
	}{
		{"the documented fixture cycle", func() *pkit.App {
			return pkit.NewApp("collect").Use(cart.Module, collectibles.Module, doors)
		}, "cart → collectibles → cart is a cycle: cart takes cartcontracts.Extension from collectibles; collectibles needs cartcontracts.Service from cart"},
		{"a cycle whose module records being built", func() *pkit.App {
			return pkit.NewApp("collect").Use(cart.Module, shelf, doors)
		}, "cart → shelf → cart is a cycle: cart takes cartcontracts.Extension from shelf; shelf needs cartcontracts.Service from cart"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := onOneDatabase(t)
			once := pkit.Deployment{Environment: pkit.Development, Config: cfg}
			validated := tc.app().Validate(once)
			says(t, validated, "Use: "+tc.want)

			_, err := tc.app().Build(t.Context(), once)
			says(t, err, validated.Error())
			if built {
				t.Error("a module on the refused cycle was built")
			}
			if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
				t.Errorf("the refused build left %d tables behind: the cycle was answered after an effect", got)
			}
		})
	}
}
