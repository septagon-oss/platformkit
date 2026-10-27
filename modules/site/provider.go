package site

import (
	"fmt"

	"github.com/septagon-oss/platformkit/design"
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
//
// The palette is not asked for: it is read from the Skin, which is where the
// application already named it with its own customisation word (`Theme(pair)`),
// the same seam modules/web's provider reads for the stylesheet it composes. It is
// answered here rather than declared as a Needs because the module that knows an
// installation's product colours is built after this one — it needs the settings
// service to put its change subject together — and a plan that waited for it would
// be a cycle. One service is put under both contracts it answers, so there is no
// second door through which a palette-less service could reach a composition.
var Module = pkit.NewModule("site", wire,
	pkit.Optional[contracts.WriteGate](),
	pkit.Provides[contracts.Service](),
	pkit.Provides[contracts.LockedReader](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	skin := w.Skin()
	svc, manifest, err := NewChecked(Deps{
		Gate:    pkit.Get[contracts.WriteGate](w),
		Palette: siteAccent(skin.Theme),
	})

	if err != nil {
		return module.Module{}, err
	}
	// One object answers both contracts and is put twice, so there is no second door
	// through which a settings service that was never given a palette could reach a
	// composition. internal.Service is built to answer both; a build in which it does
	// not is a defect in this module and fails the boot that would have hidden it.
	locked, ok := svc.(contracts.LockedReader)
	if !ok {
		return module.Module{}, fmt.Errorf("site: the settings service does not answer the locked read")
	}
	pkit.Put(w, svc)
	pkit.Put[contracts.LockedReader](w, locked)
	return manifest, nil
}

// siteAccent is the application's own pair as the port: read off the Skin once,
// at wiring, and never re-derived per request. A composition that named no pair
// has two empty accents, which is the answer contracts.Palette says an
// installation with no palette gives.
func siteAccent(pair design.Pair) contracts.Palette {
	return contracts.PaletteFunc(func(mode string) string {
		if mode == contracts.ThemeDark {
			return pair.Dark.AccentDefault
		}
		return pair.Light.AccentDefault
	})
}
