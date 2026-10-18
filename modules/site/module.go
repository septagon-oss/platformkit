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
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/site/internal"
)

// Deps is what this module cannot make for itself. It is a struct rather than no
// parameter so that the day it needs something, every call site gains a named
// field instead of a new argument.
type Deps struct {
	// Gate stands on the settings route and answers whether this tenant's settings
	// may be written directly. nil writes them, which is what every composition
	// that has not decided otherwise wants. See contracts.WriteGate for why the
	// question is asked here and answered elsewhere.
	Gate contracts.WriteGate

	// Palette answers the installation's own accent, per mode, for the site that
	// named none: the read resolves the stored empty colour through it, so nothing
	// downstream ever sees "no colour" where a colour belongs. nil is an
	// installation that names no palette of its own — such a composition answers
	// the empty colour and pins nothing. See contracts.Palette.
	Palette contracts.Palette
}

// permissions is what the manifest declares. kit/app checks every route's
// declaration against it at boot, so a route guarded by a permission that is
// not here fails startup instead of denying everyone forever.
var permissions = []module.Permission{{Key: contracts.PermissionSiteManage, Label: "manage the site"}}

// New returns the settings service and the manifest that mounts it. The one
// service answers both of its contracts — `contracts.Service` for whoever reads a
// site and `contracts.LockedReader` for the caller about to write one — because a
// second constructor could not reach the palette in Deps and would hand out a
// service that answers the empty colour where this one answers the installation's.
//
// The three routes this module used to write by hand are rest.Singleton now:
// one row per tenant, a read, a PUT, and a public face. They were the same
// three routes modules/billing wrote for its subscription, with the same
// transaction ritual and the same error mapping and no resource registered, so
// neither module had a generated screen. The kernel has the shape now, and this
// is the module that shows what it takes: a Load that answers with the defaults
// rather than a 404, a Save, and a Face saying what a visitor may see.
func New(deps Deps) (contracts.Service, module.Module) {
	svc := internal.NewService(deps.Palette)
	settings := rest.Singleton[*contracts.SiteSettings]{
		Module: "site", Entity: "settings", Path: "/settings",
		Name:  rest.Name{One: "Setting", Other: "Settings"},
		Read:  contracts.PermissionSiteManage,
		Write: contracts.PermissionSiteManage,
		// Present is how these settings read. `singular` and `plural` are the same
		// words because this resource has no plural — a tenant has one row — and
		// they are written rather than left to `Singular + "s"` precisely because
		// that default is the "1 Settingss" the phone photographs. No
		// `primaryField`, `statusField`, `summaryFields` or `sortable`: a singleton
		// has no rows, no list and no New button.
		Present: entity.EntryHints{
			Singular: "site settings",
			Plural:   "site settings",
			Icon:     "settings",
			Group:    &entity.ResourceGroup{Key: "site", Label: "Site"},
		},
		Event:  contracts.EventSettingsUpdated,
		Public: true,
		Load: func(ctx context.Context, tx db.Tx[db.Tenant]) (*contracts.SiteSettings, error) {
			return svc.Settings(ctx, tx)
		},
		Save: func(ctx context.Context, tx db.Tx[db.Tenant], in *contracts.SiteSettings) (*contracts.SiteSettings, error) {
			// The gate stands on the door a person types into, not on Service.Save.
			// That distinction is the whole use of it: the apply of a proposal comes
			// through Service.Save, and a gate that refused both would be a wall
			// rather than a door — the change could be proposed and never applied.
			if deps.Gate != nil {
				if err := deps.Gate.Check(ctx, tx); err != nil {
					return nil, err
				}
			}
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
		Migrations:  Migrations.Files,
		Adopts:      Migrations.Adopts,
		Permissions: permissions,
		Declared:    contracts.Events,
		Nav: []module.NavEntry{
			{Label: "Site", Screen: "site/settings", Permission: contracts.PermissionSiteManage},
		},
		Jobs:          nil,
		Subscriptions: nil,
		Routes:        func(s httpx.Surfaces) { settings.Mount(s) },
	}
}
