// Package shipping is a fixture module that contributes an extension to the
// cart and needs nothing: the third of the three contributors that one
// composition resolves onto the cart, which is built after all of them.
package shipping

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
)

// Module composes into the fixture apps as shipping.Module.
var Module = pkit.NewModule("shipping", build,
	pkit.Contributes[cartcontracts.Extension](),
)

func build(w *pkit.Wiring) (module.Module, error) {
	pkit.Put(w, cartcontracts.Extension{Module: "shipping"})
	return module.Module{Name: "shipping", Events: []string{"shipping.labelled"}}, nil
}
