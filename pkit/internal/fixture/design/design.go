// Package design is the fixture module that describes itself without being
// built, as the reference app collects design examples.
package design

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

// Examples are the design system's parts, named without building anything.
type Examples []string

// Module composes into the fixture apps as design.Module.
var Module = pkit.NewModule("design", build,
	pkit.Describes(Examples{"button", "field"}),
)

func build(w *pkit.Wiring) (module.Module, error) {
	return module.Module{Name: "design"}, nil
}
