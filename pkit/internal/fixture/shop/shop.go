// Package shop is one of the two fixture modules that contribute a landing
// page; homepage takes one, so the app must Choose.
package shop

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	homepagecontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/homepage/contracts"
)

// Module composes into the fixture apps as shop.Module.
var Module = pkit.NewModule("shop", build,
	pkit.Contributes[homepagecontracts.Landing](),
)

func build(w *pkit.Wiring) (module.Module, error) {
	pkit.Put(w, homepagecontracts.Landing{Path: "/shop"})
	return module.Module{Name: "shop"}, nil
}
