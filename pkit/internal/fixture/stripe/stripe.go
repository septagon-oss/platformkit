// Package stripe is one of the two fixture modules that provide the payment
// contract, so an app taking it must Choose between them.
package stripe

import (
	"strconv"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	paymentcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/payment/contracts"
)

// Module composes into the fixture apps as stripe.Module.
var Module = pkit.NewModule("stripe", build,
	pkit.Provides[paymentcontracts.Provider](),
)

type provider struct{}

func (provider) Charge(minor int64) string { return "stripe:" + strconv.FormatInt(minor, 10) }

func build(w *pkit.Wiring) (module.Module, error) {
	pkit.Put(w, paymentcontracts.Provider(provider{}))
	return module.Module{Name: "stripe"}, nil
}
