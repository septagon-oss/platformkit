package tours

import (
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/tour"
	"github.com/septagon-oss/platformkit/modules/tours/contracts"
	"github.com/septagon-oss/platformkit/modules/tours/internal"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the optional tours runtime as pkit composes it.
var Module = pkit.NewModule("tours", wire,
	pkit.Needs[contracts.Clock](),
	pkit.Needs[tenancy.Policy](),
	pkit.Provides[contracts.Service](),
	pkit.After(pkit.Everything),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	nats := pkit.Config(w, func(c config.Config) config.NATS { return c.NATS })
	app, err := nats.AppName()
	if err != nil {
		return module.Module{}, err
	}
	clock, policy := pkit.Get[contracts.Clock](w), pkit.Get[tenancy.Policy](w)
	var declarations []tour.Tour
	for _, manifest := range pkit.Composition(w) {
		declarations = append(declarations, manifest.Tours...)
	}
	svc := internal.NewService(clock, policy, app, declarations)
	pkit.Put[contracts.Service](w, svc)
	return New(Deps{Clock: clock, Policy: policy, App: app, Service: svc}), nil
}
