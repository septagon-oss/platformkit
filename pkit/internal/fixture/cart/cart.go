// Package cart is the fixture module that provides one service and takes
// every contribution of another contract.
package cart

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
)

// Module composes into the fixture apps as cart.Module.
var Module = pkit.NewModule("cart", build,
	pkit.Provides[cartcontracts.Service](),
	pkit.Needs[[]cartcontracts.Extension](),
)

type service struct{ extensions []cartcontracts.Extension }

func (s service) Total() int64 { return int64(len(s.extensions)) }

func build(w *pkit.Wiring) (module.Module, error) {
	pkit.Put(w, cartcontracts.Service(service{extensions: pkit.All[cartcontracts.Extension](w)}))
	return module.Module{Name: "cart"}, nil
}
