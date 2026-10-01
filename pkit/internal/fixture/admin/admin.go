// Package admin is the fixture module that runs after every other one and
// reads the whole composition, as the reference registry's admin does.
package admin

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	admincontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/admin/contracts"
)

// Module composes into the fixture apps as admin.Module.
var Module = pkit.NewModule("admin", build,
	pkit.After(pkit.Everything),
	pkit.Provides[admincontracts.Console](),
)

type console struct{ resources []string }

func (c console) Resources() []string { return c.resources }

func build(w *pkit.Wiring) (module.Module, error) {
	var resources []string
	for _, m := range pkit.Composition(w) {
		resources = append(resources, m.Name)
	}
	pkit.Put(w, admincontracts.Console(console{resources: resources}))
	return module.Module{Name: "admin"}, nil
}
