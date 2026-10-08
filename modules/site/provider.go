package site

import (
	"github.com/septagon-oss/platformkit/kit/module"
	contracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the site's settings as the resolver sees it, for an application that
// names it in Use: `Use(site.Module)`.
//
// The gate is Optional, and Optional is the exact word for it: nil writes the
// settings directly, which is what every composition that has not decided
// otherwise wants, and a composition that composes no provider gets Explain's
// "could use …; composes no provider, so it will not" line rather than silence.
// Which flag gates the write, and what the refusal says instead, is the
// installation's — a module that knew would be a module with a customer in it.
var Module = pkit.NewModule("site", wire,
	pkit.Optional[contracts.WriteGate](),
	pkit.Provides[contracts.Service](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	svc, manifest := New(Deps{Gate: pkit.Get[contracts.WriteGate](w)})
	pkit.Put(w, svc)
	return manifest, nil
}
