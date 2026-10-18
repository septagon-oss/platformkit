// Package audit is the second fixture module asking to run after
// everything, which is the composition's mistake to name.
package audit

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module composes into the fixture apps as audit.Module.
var Module = pkit.NewModule("audit", build,
	pkit.After(pkit.Everything),
)

func build(w *pkit.Wiring) (module.Module, error) {
	return module.Module{Name: "audit"}, nil
}
