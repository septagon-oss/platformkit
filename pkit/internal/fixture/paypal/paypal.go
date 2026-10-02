// Package paypal is the second fixture module providing the payment
// contract; an app that composes both must Choose one.
package paypal

import (
	"strconv"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	paymentcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/payment/contracts"
)

// Module composes into the fixture apps as paypal.Module.
var Module = pkit.NewModule("paypal", build,
	pkit.Provides[paymentcontracts.Provider](),
)

type provider struct{}

func (provider) Charge(minor int64) string { return "paypal:" + strconv.FormatInt(minor, 10) }

func build(w *pkit.Wiring) (module.Module, error) {
	pkit.Put(w, paymentcontracts.Provider(provider{}))
	return module.Module{Name: "paypal"}, nil
}
