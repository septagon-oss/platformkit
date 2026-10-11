// Package blog is the second fixture module contributing a landing page.
package blog

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	homepagecontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/homepage/contracts"
)

// Module composes into the fixture apps as blog.Module.
var Module = pkit.NewModule("blog", build,
	pkit.Contributes[homepagecontracts.Landing](),
)

func build(w *pkit.Wiring) (module.Module, error) {
	pkit.Put(w, homepagecontracts.Landing{Path: "/blog"})
	return module.Module{Name: "blog"}, nil
}
