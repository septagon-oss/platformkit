// Package web is the public site: what an anonymous visitor sees at the root
// of a tenant's host. It renders what the site module's settings and the
// content module's published pages say, and writes nothing of its own — a
// shell composed from ui/page and ui/components the way the admin is, with a
// bar where the admin has a sidebar, and no controller of its own: the only
// script a page may carry is the shared snippet that applies a theme the
// visitor stored, and a page whose theme the tenant pinned carries none.
//
// It is composed in the reference application and left out of one that has a
// storefront of its own: the root belongs to one module, and two claiming it
// is a boot failure rather than a coin toss. The module has no permissions, no
// events, no jobs and no table; everything it shows is another module's.
package web

import (
	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/web/internal"
)

// Deps is what the site reads: the settings, the published pages, and the
// palette the stylesheet is composed from. Both services are required.
type Deps struct {
	Site    sitecontracts.Service
	Content contentcontracts.Service
	Theme   design.Pair
}

// Module is the manifest: two public routes and nothing else to declare.
func Module(deps Deps) module.Module {
	if deps.Site == nil || deps.Content == nil {
		panic("web: Deps.Site and Deps.Content are required; the site renders what they publish")
	}
	return module.Module{
		Name:          "web",
		Permissions:   nil,
		Events:        nil,
		Jobs:          nil,
		Subscriptions: nil,
		Routes: func(api *httpx.API) {
			internal.Mount(api, internal.Site{Settings: deps.Site, Content: deps.Content, Theme: deps.Theme})
		},
	}
}
