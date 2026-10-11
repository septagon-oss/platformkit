// Package wishlist is the fixture module that resolves honestly: it needs a
// service, contributes an extension, and uses a payment provider only when
// the app composes one.
package wishlist

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
	paymentcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/payment/contracts"
	usercontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/user/contracts"
)

// Module composes into the fixture apps as wishlist.Module.
var Module = pkit.NewModule("wishlist", build,
	pkit.Needs[usercontracts.Service](),
	pkit.Optional[paymentcontracts.Provider](),
	pkit.Contributes[cartcontracts.Extension](),
)

func build(w *pkit.Wiring) (module.Module, error) {
	owner := pkit.Get[usercontracts.Service](w).Name("owner")
	if pkit.Get[paymentcontracts.Provider](w) != nil {
		owner += " with payment"
	}
	pkit.Put(w, cartcontracts.Extension{Module: owner})
	return module.Module{Name: "wishlist"}, nil
}
