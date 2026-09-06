// Package site is the module manifest: the data a tenant's public site is made
// of, and none of the rendering.
//
// It is the smallest module here, and the three absences are the interesting
// part. There is no rest.Spec, because a Spec is five routes on a collection
// and a tenant has one site: the settings are a singleton, so the routes are a
// read and a PUT. There is no job, because nothing about a site happens because
// time passed. And there is no HTML — a theme reads a title, a navigation and a
// colour and decides what to do with them, which is E4's admin shell and
// whatever public theme follows it. This module owns the data that theme reads,
// and owning it separately is what lets the theme be replaced.
package site

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/site/internal"
)

// Deps is what this module cannot make for itself, and it is empty: a site
// belongs to a tenant by carrying its id, which row-level security matches on.
// It is a struct rather than no parameter so that the day it needs something,
// every call site gains a named field instead of a new argument.
type Deps struct{}

// permissions is what the manifest declares. kit/app checks every route's
// declaration against it at boot, so a route guarded by a permission that is
// not here fails startup instead of denying everyone forever.
var permissions = []module.Permission{{Key: contracts.PermissionSiteManage}}

// Module is the manifest, and the service it is built on.
//
// The three routes this module used to write by hand are rest.Singleton now:
// one row per tenant, a read, a PUT, and a public face. They were the same
// three routes modules/billing wrote for its subscription, with the same
// transaction ritual and the same error mapping and no resource registered, so
// neither module had a generated screen. The kernel has the shape now, and this
// is the module that shows what it takes: a Load that answers with the defaults
// rather than a 404, a Save, and a Face saying what a visitor may see.
func Module(_ Deps) (contracts.Service, module.Module) {
	svc := internal.NewService()
	settings := rest.Singleton[*contracts.SiteSettings]{
		Module: "site", Entity: "settings", Path: "/api/v1/site/settings",
		Read:   contracts.PermissionSiteManage,
		Write:  contracts.PermissionSiteManage,
		Event:  contracts.EventSettingsUpdated,
		Public: true,
		Load: func(ctx context.Context, tx db.Tx[db.Tenant]) (*contracts.SiteSettings, error) {
			return svc.Settings(ctx, tx)
		},
		Save: func(ctx context.Context, tx db.Tx[db.Tenant], in *contracts.SiteSettings) (*contracts.SiteSettings, error) {
			return svc.Save(ctx, tx, in)
		},
		// The name, the navigation and the colour scheme. The rest — the home
		// slug, the logo, the timestamps — is either an internal reference or
		// nobody's business.
		Face: func(s *contracts.SiteSettings) any {
			nav := s.Nav
			if nav == nil {
				// A navigation is a list, and a JSON null where a list belongs
				// is a theme's null dereference. An empty site has an empty one.
				nav = contracts.Nav{}
			}
			return &contracts.Public{Title: s.Title, Nav: nav, Theme: s.Theme}
		},
	}
	return svc, module.Module{
		Name:        "site",
		Permissions: permissions,
		Events:      contracts.Events,
		Nav: []module.NavEntry{
			{Label: "Site", Path: "/admin/site/settings", Permission: contracts.PermissionSiteManage},
		},
		Jobs:          nil,
		Subscriptions: nil,
		Routes:        settings.Mount,
	}
}
