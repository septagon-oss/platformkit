// Package payment is the fixture module whose implementation the deployment
// picks: Stripe with its inputs, or the local simulation in development.
package payment

import (
	"strconv"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	paymentcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/payment/contracts"
)

// Module composes into the fixture apps as payment.Module.
var Module = pkit.NewModule("payment", build,
	pkit.Provides[paymentcontracts.Provider](),
	pkit.FromDeployment(
		pkit.Implementation{Name: "stripe", Inputs: []string{"stripe.key", "stripe.webhook.secret"}},
		pkit.Implementation{Name: "manual", Simulated: true},
	),
)

type provider string

func (p provider) Charge(minor int64) string { return string(p) + ":" + strconv.FormatInt(minor, 10) }

func build(w *pkit.Wiring) (module.Module, error) {
	pkit.Put(w, paymentcontracts.Provider(provider(w.Implementation())))
	return module.Module{Name: "payment"}, nil
}
