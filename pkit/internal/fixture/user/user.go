// Package user is the fixture module that provides a service and needs
// nothing.
package user

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	usercontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/user/contracts"
)

// Module composes into the fixture apps as user.Module.
var Module = pkit.NewModule("user", build,
	pkit.Provides[usercontracts.Service](),
)

type service struct{}

func (service) Name(id string) string { return "person " + id }

func build(w *pkit.Wiring) (module.Module, error) {
	pkit.Put(w, usercontracts.Service(service{}))
	return module.Module{Name: "user"}, nil
}
