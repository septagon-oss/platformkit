// Package homepage is the fixture module that takes exactly one landing
// page and is built after whoever contributes it.
package homepage

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	homepagecontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/homepage/contracts"
)

// Module composes into the fixture apps as homepage.Module.
var Module = pkit.NewModule("homepage", build,
	pkit.Needs[homepagecontracts.Landing](),
)

func build(w *pkit.Wiring) (module.Module, error) {
	_ = pkit.Get[homepagecontracts.Landing](w)
	return module.Module{Name: "homepage"}, nil
}
