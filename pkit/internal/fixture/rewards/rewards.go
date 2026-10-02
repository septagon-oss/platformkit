// Package rewards is a fixture module that contributes an extension to the
// cart and nothing else: with wishlist and gallery it is one of the three
// contributors to the one consumer that takes every extension.
package rewards

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
)

// Module composes into the fixture apps as rewards.Module.
var Module = pkit.NewModule("rewards", build,
	pkit.Contributes[cartcontracts.Extension](),
)

func build(w *pkit.Wiring) (module.Module, error) {
	pkit.Put(w, cartcontracts.Extension{Module: "rewards"})
	return module.Module{Name: "rewards", Permissions: []module.Permission{{Key: "rewards:read", Label: "read rewards"}}}, nil
}
