// Package collectibles is the fixture module whose claims form the planted
// failures: it needs the cart's service, contributes an extension to it,
// and uses a payment provider only if the app composes one.
package collectibles

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
	paymentcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/payment/contracts"
)

// Module composes into the fixture apps as collectibles.Module.
var Module = pkit.NewModule("collectibles", build,
	pkit.Needs[cartcontracts.Service](),
	pkit.Contributes[cartcontracts.Extension](),
	pkit.Optional[paymentcontracts.Provider](),
)

func build(w *pkit.Wiring) (module.Module, error) {
	name := "collectibles"
	if pkit.Get[cartcontracts.Service](w) != nil && pkit.Get[paymentcontracts.Provider](w) != nil {
		name = "collectibles with payment"
	}
	pkit.Put(w, cartcontracts.Extension{Module: name})
	return module.Module{Name: "collectibles"}, nil
}
