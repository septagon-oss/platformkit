package web

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	webcontracts "github.com/septagon-oss/platformkit/modules/web/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the public site as the resolver sees it, for an application that
// names it in Use: `Use(web.Module)`. It hands out nothing and claims the root.
//
// The two services it renders come by need, so the modules that publish them are
// built first and withdrawing either is a refusal naming the contract and the
// taker. The palette and the refusal copy come from the app's own Skin — the
// theme it named with Theme and the words it named with Languages — which is why
// a shell and a site cannot be worded from two different files, and why no
// composition file carries a design.Pair to the module that draws with one.
//
// The two addresses it links and does not serve are an Optional Links: a
// composition with no shell composes no provider and gets a site with no
// sign-in link, which is what a headless storefront wants.
//
// So is the pair that makes a page answer in a language other than the one it was
// authored in: a site whose application names no translation module is a
// monolingual site, and it says so in its `lang` attribute rather than promising
// an alternate it cannot serve.
var Module = pkit.NewModule("web", wire,
	pkit.Needs[sitecontracts.Service](),
	pkit.Needs[contentcontracts.Service](),
	pkit.Needs[richtext.Files](),
	pkit.Optional[webcontracts.Links](),
	pkit.Optional[rest.Translations](),
	pkit.Optional[rest.TranslationSource](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	links := pkit.Get[webcontracts.Links](w)
	skin := w.Skin()
	return New(Deps{
		Site:    pkit.Get[sitecontracts.Service](w),
		Content: pkit.Get[contentcontracts.Service](w),
		Files:   pkit.Get[richtext.Files](w),
		Theme:   skin.Theme,
		// The refusal sentences the site's two addresses can answer with are the
		// kernel layer's, so the site shows them to a visitor in the language the
		// tenant is served in. What the site writes itself — the bar, the footer,
		// the empty states — stays in the source language and says so.
		Messages:      skin.Copy,
		SignInPath:    links.SignIn,
		PublicFileURL: links.PublicFile,
		// The page's other languages, or nothing. Both come from the same
		// composition — the port that holds the text and the row set that says
		// which entity is a page — and a site handed one without the other
		// renders monolingual rather than guessing, which is what Site.serve
		// checks. An installation with no translation module composed gets
		// exactly the site it had: every page in the language it was authored
		// in, no `hreflang`, no claims.
		Translations: pkit.Get[rest.Translations](w),
		ContentRows:  pkit.Get[rest.TranslationSource](w),
	}), nil
}
