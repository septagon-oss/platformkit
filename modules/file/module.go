// Package file is the module manifest: uploaded bytes and the rows that name
// them.
//
// It is the shape every module follows, with the one asymmetry a module about
// bytes has: there is no rest.Spec, because a Spec's create route takes a JSON
// body and a file arrives as a stream. The six routes are written out in
// internal/handler.go.
//
// The thing to read before the rest is where a blob write sits relative to a
// commit, because it is the only interesting problem here and it has two
// different answers.
//
// An upload writes the bytes first and the row second: a transaction that then
// fails leaves bytes nobody references, which costs disk, where the other order
// would leave a row that references nothing, which is a download that fails
// forever.
//
// A delete writes the row first and the bytes never: it publishes file.deleted
// and this module subscribes to its own event, so the blob goes once the
// transaction that removed the row has committed. Removing it inside that
// transaction would be a delete a rollback cannot undo — the row would come
// back and the bytes would be gone.
package file

import (
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// Local is contracts.Storage on the filesystem. main wires it, or whatever a
// deployment has instead, so the choice is visible in the file that composes the
// application — the same way the mailer and the payment provider are.
var Local = internal.NewLocal

// DefaultMaxBytes is the largest upload a deployment that says nothing accepts.
// Twenty-five megabytes is what a mail attachment limit taught everybody to
// expect, and config.Files is where a deployment says otherwise.
const DefaultMaxBytes = 25 << 20

// Deps is what this module cannot make for itself.
type Deps struct {
	// Storage is where the bytes go. There is one implementation here, Local;
	// the ones that speak to an object store live outside this repository.
	Storage contracts.Storage

	// MaxBytes is the largest upload accepted; zero means DefaultMaxBytes. It
	// is a dependency rather than a constant because how much disk a deployment
	// is willing to take from an anonymous mistake is a deployment's decision.
	MaxBytes int64
}

// permissions is what the manifest declares. kit/app checks every route's
// declaration against it at boot, so a route guarded by a permission that is
// not here fails startup instead of denying everyone forever.
var permissions = []module.Permission{
	{Key: contracts.PermissionFileRead},
	{Key: contracts.PermissionFileManage},
}

// Module is the manifest. The implementation is constructed here, in one line,
// and handed to the two places that use it.
func Module(deps Deps) module.Module {
	if deps.Storage == nil {
		// A wiring mistake fails where it is written rather than as a nil
		// dereference on the first upload.
		panic("file.Module: Deps.Storage is required; wire file.Local(dir) to keep the bytes on disk")
	}
	max := deps.MaxBytes
	if max <= 0 {
		max = DefaultMaxBytes
	}
	svc := internal.NewService(deps.Storage, max)
	return module.Module{
		Name:        "file",
		Permissions: permissions,
		Events:      contracts.Events,
		Nav: []module.NavEntry{
			{Label: "Files", Path: "/admin/file/files", Permission: contracts.PermissionFileRead},
		},
		// No periodic work: nothing about a file happens because time passed.
		Jobs: nil,
		// The one subscription, and it is to this module's own event: the bytes
		// are removed after the transaction that removed the row commits, and
		// an event is the only thing delivered exactly then.
		Subscriptions: []events.Subscription{internal.RemoveBlob(deps.Storage)},
		Routes:        func(api *httpx.API) { internal.RegisterRoutes(api, svc) },
	}
}
