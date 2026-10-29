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
	"errors"
	"time"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
	"github.com/septagon-oss/platformkit/ui/page"
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

	// MaxImagePixels is the largest frame the image pass decodes: a file whose
	// header claims more is refused rather than allocated. Zero means
	// contracts.DefaultMaxImagePixels. It is a dependency for the reason
	// MaxBytes is — one deployment's users send phone photos and another's send
	// scans of posters — and it bounds a decode rather than a download.
	MaxImagePixels int

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

	// VariantEvery replaces the encode pass that builds a stored image's other
	// widths with an interval, for a deployment that will not wait five minutes
	// and for a test that cannot wait at all. Zero means the schedule.
	VariantEvery time.Duration

	// UnusedAfter is the grace period an image is kept after nothing is showing
	// it, and zero means internal.DefaultUnusedAfter (a day). It is a dependency
	// and not a constant because how long a person gets to move a picture from one
	// draft to another without paying for its storage twice is a deployment's
	// call; a negative number is not "delete immediately" but no sweep at all.
	UnusedAfter time.Duration

	// UnusedEvery replaces the daily release sweep with an interval, for a test
	// that cannot wait until ten to five in the morning. Zero means the schedule.
	UnusedEvery time.Duration

	// Library is the media library's page: the shell it is drawn in, whose answer
	// to "may this caller write" its two controls reuse, and how many tiles one
	// page holds. Nil, the composition serves no library — the manifest's Nav entry
	// below then names a screen nobody mounted, which is why the composition that
	// ships this module hands the chrome in rather than leaving the entry dangling.
	//
	// What the page reads is not among it, on purpose: the grid, the meter and the
	// caption are the one service built below, and a composition that could name
	// them could name a library reading a different service than the doors beside it.
	Library *LibraryChrome
}

// LibraryChrome is what a composition hands this module so its one screen can be
// drawn. The service is filled by this module, from the one it builds; what the
// application owns is the chrome and the authorizer, which is what this edge carries
// and nothing more.
type LibraryChrome struct {
	Shell     page.Shell
	Authorize httpx.Authorizer
	PerPage   int
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
//
// Deprecated: use NewChecked, which answers this refusal as an error.
func New(deps Deps) (contracts.Service, module.Module) {
	svc, manifest, err := NewChecked(deps)
	if err != nil {
		panic(err.Error())
	}
	return svc, manifest
}

// NewChecked builds the service and the manifest, and answers as an error the
// wiring it cannot work without: Deps.Storage is required, and Deps.Tenants is
// required once Deps.Retention names a class. A zero MaxBytes, QuotaBytes,
// MaxImagePixels or interval keeps the default beside it. It opens nothing, runs
// no query and is safe to call twice.
func NewChecked(deps Deps) (contracts.Service, module.Module, error) {
	if deps.Storage == nil {
		// Storage is required before any route can accept an upload.
		return nil, module.Module{}, errors.New("file.Module: Deps.Storage is required; wire file.Local(dir) to keep the bytes on disk")
	}
	if len(deps.Retention) > 0 && deps.Tenants == nil {
		// The other half of the same rule. A retention policy is a promise that
		// bytes get removed; the sweep keeps it by walking the tenants, and a
		// module handed a policy and no way to walk is a module that will not.
		return nil, module.Module{}, errors.New("file.Module: Deps.Tenants is required when Deps.Retention names a class; wire tenant.Active so the sweep can walk them")
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
	svc := internal.NewService(deps.Storage, max, quota, deps.MaxImagePixels)
	sweep, scheduled := fileJobs(deps, svc)
	library := fileLibrary(deps, svc)
	return svc, fileModule(deps, svc, library, sweep, scheduled), nil
}

func fileLibrary(deps Deps, svc *internal.Service) internal.LibraryPages {
	// The library's page is composed over the service this function just built, so
	// the meter on it and the refusal in an upload are the same sum, and the grid
	// reads through the same row-level policy the list door answers by. No chrome
	// means no page: the surface's own 404 for a screen nobody composed.
	var library internal.LibraryPages
	if deps.Library != nil {
		library = internal.LibraryPages{
			Shell: deps.Library.Shell, Authorize: deps.Library.Authorize, PerPage: deps.Library.PerPage,
			Read: svc, Shown: svc, Remove: svc, Add: svc, Widths: svc,
		}
	}
	return library
}

func fileModule(deps Deps, svc *internal.Service, library internal.LibraryPages, sweep *internal.Reconcile, scheduled []jobs.Job) module.Module {
	return module.Module{
		Name:       "file",
		Views:      contracts.Views(deps.Library != nil, true, true),
		Migrations: Migrations.Files,
		// Bytes go, rows stay, and the certificate names both a subject and an actor.
		PersonalData: filePersonalData(),
		Adopts:       Migrations.Adopts,
		Permissions:  permissions,
		Declared:     contracts.Events,
		Nav: []module.NavEntry{
			{Label: "Files", Screen: "file/files", Permission: contracts.PermissionFileRead},
		},
		// The periodic work, and it is four things now. The orphan sweep is the
		// cost of writing the bytes before the row: a transaction that failed after
		// the blob was written left bytes nobody references, and this removes them.
		// The retention sweep removes rows whose class ran out. The encode pass
		// builds the widths of a stored image, which is the one job here that writes
		// bytes rather than removing them — and the reason it is on the clock is that
		// an upload's own request should answer with the frame it was given rather
		// than with four encodes on the way there. The release sweep removes the images
		// nobody is showing once the grace period has run — the one job that reads the
		// use ledger rather than writing it.
		Jobs: scheduled,
		// The one subscription, and it is to this module's own event: the bytes
		// are removed after the transaction that removed the row commits, and
		// an event is the only thing delivered exactly then.
		Subscriptions: []events.Subscription{internal.EraseBlobs(deps.Storage)},
		// The sweep walks the object store and asks which rows still point at
		// each blob, which is a question across every tenant: the bytes exist
		// whether or not the row that owns them does. See docs/adr/0006.
		SystemAccess: module.SystemAccess{Reason: "sweep stored bytes no row references, across every tenant"},
		Routes: func(s httpx.Surfaces) {
			sweep.Use(s.Ops.SystemToken())
			internal.RegisterRoutes(s, svc)
			// The library page, over the one service the doors above are mounted on —
			// so the grid's meter and the upload's refusal cannot be two sums.
			internal.MountLibrary(s, library)
		},
	}
}

// fileJobs builds the scheduled work and returns the store-facing sweep for its
// route capability. Each job owns a distinct cleanup or encoding boundary.
func fileJobs(deps Deps, svc *internal.Service) (*internal.Reconcile, []jobs.Job) {
	// Reconciliation starts at the store because orphaned bytes have no tenant row
	// to walk from. The other sweeps start from rows and use tenant transactions.
	sweep := internal.NewReconcile(deps.Storage, deps.ReconcileEvery)
	retention := internal.NewSweep(deps.Storage, internal.SweepConfig{
		Retention: deps.Retention, Tenants: deps.Tenants, Every: deps.RetainEvery,
	})
	variants := internal.NewVariants(svc, deps.Storage, deps.Tenants, deps.VariantEvery)
	unused := internal.NewUnused(deps.Tenants, deps.UnusedAfter, deps.UnusedEvery)
	all := append(sweep.Jobs(), retention.Jobs()...)
	all = append(all, variants.Jobs()...)
	all = append(all, unused.Jobs()...)
	return sweep, all
}

// filePersonalData maps the module's stored text and references to their
// subject or related record for erasure.
func filePersonalData() personaldata.Declaration {
	return personaldata.Implements(
		// files.alt (000063) is the sentence somebody typed about a picture, and a
		// caption is their own words about themselves as much as a file name is — with
		// the difference that the caption is what a screen reader hands out to whoever
		// cannot see the picture. It is claimed beside `name` and scrubbed with it, in
		// the one statement that also moves the row's instant: see personalData.Erase.
		personaldata.Table{Name: "files", Person: "uploader_id", Text: []string{"name", "alt"}},
		personaldata.Table{Name: "file_holds", Person: "placed_by", Text: []string{"reason"}, NotPerson: []string{"file_id"}},
		personaldata.Table{Name: "file_erasures", Person: "subject_id", About: []string{"actor"}, Text: []string{"reason"}, NotPerson: []string{"file_id"}},
		// file_uses (000045) points at another module's record, so record is about
		// that person; the vocabulary columns and language tag hold no identifiers.
		personaldata.Table{Name: "file_uses", About: []string{"record"}, NotPerson: []string{"file_id"}},
		// A variant belongs to its file row, not directly to a person.
		personaldata.Table{Name: "file_variants", NotPerson: []string{"file_id"}},
	)
}
