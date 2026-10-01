// Package reports is the fixture module that needs the admin's console —
// which is the mistake of needing a module that runs after everything.
package reports

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	admincontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/admin/contracts"
)

// Module composes into the fixture apps as reports.Module.
var Module = pkit.NewModule("reports", build,
	pkit.Needs[admincontracts.Console](),
)

func build(w *pkit.Wiring) (module.Module, error) {
	_ = pkit.Get[admincontracts.Console](w)
	return module.Module{Name: "reports"}, nil
}
