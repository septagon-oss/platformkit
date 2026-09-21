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

	// SignInPath is where a visitor who wants to sign in is sent. The site has
	// no sign-in of its own — the auth module mints the session and the shell
	// owns the form — so the address belongs to whoever composed the two, and
	// this is the line that says where the workspace ended up. A literal here
	// would be this module naming a surface it does not serve, which is why the
	// composition writes it and app_test.go asks the running server that the
	// address it wrote is the address that answers.
	SignInPath string

	// PublicFileURL answers the address of a file a visitor may see, which is
	// the file module's business and this composition's knowledge. The logo is
	// the one file the site renders.
	PublicFileURL func(id string) string
}

// Module is the manifest: two public routes and nothing else to declare.
func Module(deps Deps) module.Module {
	if deps.Site == nil || deps.Content == nil {
		panic("web: Deps.Site and Deps.Content are required; the site renders what they publish")
	}
	if deps.SignInPath == "" || deps.PublicFileURL == nil {
		panic("web: Deps.SignInPath and Deps.PublicFileURL are required; the site links a sign-in it does not serve and a file it does not store")
	}
	return module.Module{
		Name:          "web",
		Permissions:   nil,
		Events:        nil,
		Jobs:          nil,
		Subscriptions: nil,
		Routes: func(r httpx.Surfaces) {
			internal.Mount(r, internal.Site{
				Settings: deps.Site, Content: deps.Content, Theme: deps.Theme,
				SignIn: deps.SignInPath, File: deps.PublicFileURL,
			})
		},
	}
}
