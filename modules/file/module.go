// Package file is the module manifest: uploaded bytes and the rows that name
// them.
//
// It is the shape every module follows, with the one asymmetry a module about
// bytes has: there is no rest.Spec, because a Spec's create route takes a JSON
// body and a file arrives as a stream. The twelve operations over seven paths are
// written out in internal/handler.go.
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
	"time"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// Local is contracts.Storage on the filesystem. main wires it, or whatever a
// deployment has instead, so the choice is visible in the file that composes the
// application — the same way the mailer and the payment provider are.
var Local = internal.NewLocal

// S3 is contracts.Storage and contracts.Signer on any store that speaks the S3
// API — AWS S3, Garage, SeaweedFS, Ceph RGW — through minio-go. Wiring it is the
// same call made in the same place Local is wired.
//
// What it buys over Local is the shape a shared-instance deployment (decision
// 0028) needs — one bucket holding every tenant's bytes, with the tenant in every
// object's name rather than in a directory on one volume — and a working grant
// route, because it implements the Signer Local cannot: a private byte is then
// served by a presigned URL with no request of this process in the path. What it
// costs is stated on internal.S3: no orphan sweep (a bucket lifecycle rule's) and
// no counted erasure proof (verified_at stays NULL).
var S3 = internal.NewS3

// S3Config is where such a store is and how to reach it.
type S3Config = internal.S3Config

// DefaultQuotaBytes is the disk one tenant may fill when a deployment says
// nothing. A gigabyte is a number a person can reason about — a thousand
// documents, or a hundred photographs — and config.Files is where a deployment
// says otherwise. Zero means no quota, which is what a single-tenant
// installation wants and what a public sign-up must not have.
//
// The largest one upload may be is config.DefaultFilesMaxBytes, in kit/config,
// which is the only place it is written: this module used to declare the same
// 25 << 20 beside it, and two constants for one number is one of them going
// stale.
const DefaultQuotaBytes = 1 << 30

// Deps is what this module cannot make for itself.
type Deps struct {
	// Storage is where the bytes go. This module ships two: Local, the one on
	// disk, and S3, the one that speaks S3 to an object store and can sign a
	// grant. A deployment writes the one it has into this field.
	Storage contracts.Storage

	// MaxBytes is the largest upload accepted; zero means the kernel's
	// config.DefaultFilesMaxBytes. It is a dependency rather than a constant
	// because how much disk a deployment is willing to take from an anonymous
	// mistake is a deployment's decision.
	MaxBytes int64

	// QuotaBytes is the disk one tenant may hold; zero means
	// DefaultQuotaBytes and a negative number means no quota at all. It is
	// enforced at upload, against the sum of what the tenant already holds.
	QuotaBytes int64

	// ReconcileEvery replaces the daily orphan sweep with an interval, for a
	// test that cannot wait until four in the morning. Zero means the schedule.
	ReconcileEvery time.Duration

	// Retention is how long each class of file lives, keyed by the kind an
	// upload carried. It is a map and not a list of columns because a class is
	// a product's word and a duration is a deployment's: this module matches
	// one against the other and interprets neither. A kind that is not here is
	// never deleted, only logged.
	//
	// A file-only setting: kit/config's keys table reaches string scalars, so a
	// map is config-file and composition, and no environment variable overrides
	// one class.
	Retention map[string]time.Duration

	// Tenants is what the retention sweep walks. It is required whenever a
	// policy is, because a sweep that cannot list the tenants silently never
	// removes anything, and a retention policy that quietly does not run is the
	// failure mode nobody notices — the same reason Storage is required and the
	// panic below says so by name.
	Tenants jobs.TenantLister

	// RetainEvery replaces the daily retention sweep with an interval, for a
	// test. Zero means the schedule.
	RetainEvery time.Duration
}

// permissions is what the manifest declares. kit/app checks every route's
// declaration against it at boot, so a route guarded by a permission that is
// not here fails startup instead of denying everyone forever.
var permissions = []module.Permission{
	{Key: contracts.PermissionFileRead, Label: "read files"},
	{Key: contracts.PermissionFileManage, Label: "manage files"},
	{Key: contracts.PermissionFileErase, Label: "erase files"},
	{Key: contracts.PermissionFileRetain, Label: "retain files"},
}

// Module is the manifest, and the service beside it.
//
// It returns both, as user, notification, task and the catalogue's own modules
// do, because a consuming module needs the service and cannot reach into
// internal/ for it. Before this it returned only the manifest, and a client
// module that wanted to open a stored file had to call this module's own HTTP
// route — which meant holding file:manage to do something the interface says it
// may do with a transaction. contracts.Opener is the narrow half most consumers
// should take.
func Module(deps Deps) (contracts.Service, module.Module) {
	if deps.Storage == nil {
		// A wiring mistake fails where it is written rather than as a nil
		// dereference on the first upload.
		panic("file.Module: Deps.Storage is required; wire file.Local(dir) to keep the bytes on disk")
	}
	if len(deps.Retention) > 0 && deps.Tenants == nil {
		// The other half of the same rule. A retention policy is a promise that
		// bytes get removed; the sweep keeps it by walking the tenants, and a
		// module handed a policy and no way to walk is a module that will not.
		panic("file.Module: Deps.Tenants is required when Deps.Retention names a class; wire tenant.Active so the sweep can walk them")
	}
	max := deps.MaxBytes
	if max <= 0 {
		max = config.DefaultFilesMaxBytes
	}
	quota := deps.QuotaBytes
	switch {
	case quota == 0:
		quota = DefaultQuotaBytes
	case quota < 0:
		quota = 0 // said out loud: this deployment has no quota
	}
	svc := internal.NewService(deps.Storage, max, quota)
	// The sweep is constructed here and given its capability in Routes, which
	// is the one moment the kernel offers one: a job is built before the API
	// exists. See internal/reconcile.go for why it is not jobs.PerTenant.
	sweep := internal.NewReconcile(deps.Storage, deps.ReconcileEvery)
	// The retention sweep is the second job and the opposite one: the orphan
	// sweep starts at the store, this one starts at the rows, inside each
	// tenant's own transaction. internal/retention.go says why they are two
	// jobs and not two modes.
	retention := internal.NewSweep(deps.Storage, internal.SweepConfig{
		Retention: deps.Retention, Tenants: deps.Tenants, Every: deps.RetainEvery,
	})
	return svc, module.Module{
		Name:        "file",
		Migrations:  Migrations.Files,
		Adopts:      Migrations.Adopts,
		Permissions: permissions,
		Declared:    contracts.Events,
		Nav: []module.NavEntry{
			{Label: "Files", Screen: "file/files", Permission: contracts.PermissionFileRead},
		},
		// The one piece of periodic work, and it is the cost of writing the
		// bytes before the row: a transaction that failed after the blob was
		// written left bytes nobody references, and this removes them.
		Jobs: append(sweep.Jobs(), retention.Jobs()...),
		// The one subscription, and it is to this module's own event: the bytes
		// are removed after the transaction that removed the row commits, and
		// an event is the only thing delivered exactly then.
		Subscriptions: []events.Subscription{internal.EraseBlobs(deps.Storage)},
		Routes: func(s httpx.Surfaces) {
			sweep.Use(s.Ops.SystemToken())
			internal.RegisterRoutes(s, svc)
		},
	}
}
