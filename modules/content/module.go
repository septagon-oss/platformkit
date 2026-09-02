// Package content is the module manifest: the pages and posts a tenant's public
// site is made of.
//
// It is the shape every module follows: contracts/, internal/, and one exported
// function taking a struct of typed dependencies and returning a module.Module.
// Two absences are decisions rather than omissions.
//
// There are no versions. A history table, a diff, a restore and a retention
// rule are a module of their own, and the private catalogue has one; what a
// reference architecture owes is the lifecycle — draft, published, archived —
// and the events that announce it.
//
// There is no stored HTML. The body is Markdown and the public route renders it
// on read, through goldmark with raw HTML left out and bluemonday over the
// result, so a policy that tightens applies to everything ever written instead
// of to whatever is saved next. See internal/render.go.
package content

import (
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/content/internal"
)

// Deps is what this module cannot make for itself, and it is empty: content
// belongs to a tenant by carrying its id, which row-level security matches on,
// and the author comes off the request's own context. It is a struct rather
// than no parameter so that the day it needs something, every call site gains a
// named field instead of a new argument.
type Deps struct{}

// spec is the entity's presence in the application: five routes, two
// permissions, three events and the schema a generated screen reads.
var spec = rest.Spec[*contracts.Content]{
	Module:     "content",
	Entity:     "content",
	Path:       "/api/v1/content/contents",
	Read:       contracts.PermissionContentRead,
	Write:      contracts.PermissionContentManage,
	SoftDelete: true,
	// The three fields a route of its own owns. status and publishedAt belong
	// to publish, unpublish and archive, which move them together and announce
	// it; author belongs to the create that stamped it from the caller, and a
	// patch that could rewrite it would forge a byline.
	Immutable: []string{"status", "publishedAt", "author"},
	// Nothing here publishes from a hook, so there is nothing for a Spec to
	// declare on the write routes' behalf. The lifecycle's three events are
	// declared by the three commands that publish them, in internal/handler.go.
	HookEvents: nil,
}

// permissions is what the manifest declares. kit/app checks every route's
// declaration against it at boot, so a route guarded by a permission that is
// not here fails startup instead of denying everyone forever.
var permissions = []module.Permission{
	{Key: contracts.PermissionContentRead},
	{Key: contracts.PermissionContentManage},
}

// Module is the manifest. The implementation is constructed here, in one line,
// and handed to the one place that uses it.
func Module(_ Deps) module.Module {
	svc := internal.NewService()
	return module.Module{
		Name:        "content",
		Permissions: permissions,
		Events:      contracts.Events,
		Nav: []module.NavEntry{
			{Label: "Content", Path: "/admin/content/contents", Permission: contracts.PermissionContentRead},
		},
		// No periodic work: nothing about a page happens because time passed.
		// No subscriptions: this module has no opinion about anybody else's
		// events, and the site that reads it takes the read route.
		Jobs:          nil,
		Subscriptions: nil,
		Routes: func(api *httpx.API) {
			spec.Mount(api)
			internal.RegisterRoutes(api, spec, svc)
		},
	}
}
