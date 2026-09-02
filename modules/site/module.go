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
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
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
func Module(_ Deps) module.Module {
	svc := internal.NewService()
	return module.Module{
		Name:        "site",
		Permissions: permissions,
		Events:      contracts.Events,
		Nav: []module.NavEntry{
			{Label: "Site", Path: "/admin/site/settings", Permission: contracts.PermissionSiteManage},
		},
		Jobs:          nil,
		Subscriptions: nil,
		Routes:        func(api *httpx.API) { internal.RegisterRoutes(api, svc) },
	}
}
