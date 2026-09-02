package main

import (
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/task"
)

// modules is the composition: every module the application is made of, in
// dependency order, each constructed with a struct of typed dependencies.
//
// This is the whole wiring graph and there is nothing else to read. A module
// that needs another module's capability takes an interface from that module's
// contracts/ package and is handed the value here, so the order of this list is
// the startup order, a cycle cannot be expressed, and a dependency somebody
// forgot is a compile error on the line that forgot it. See docs/adr/0002.
//
// It is a function rather than a package-level slice because the dependencies
// come from the configuration: today that is the development tenant list, and
// in E3 it is the tenant module, constructed above the modules that need it.
func modules(tenants jobs.TenantLister) []module.Module {
	return []module.Module{
		task.Module(task.Deps{Tenants: tenants}),
	}
}
