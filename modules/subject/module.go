// Package subject is the manifest: a person's two doors, in one module.
//
// Everything the module knows about a person it is handed. Each owner of a person's
// rows contributes a `personaldata.Part` — user, auth, notification, file, task,
// content, change, audit — and this module asks each of them, in module-name order,
// for its own export and its own erasure. It reads exactly one thing across the
// boundary itself, the person's own row, through the user module's own contract,
// because an erasure that does not know the address and the name cannot ask the
// owners to stop finding them (ADR 0020 §1).
//
// The door is here too. A request is a row this module owns and the bundle is work the
// worker does: the POST files a `requested` row and an event in one commit, the job walks
// the parts, stores the zip through the file module and holds it, tells the person, and
// the person mints a seven-day single-use link that streams the bundle back out
// (ADR 0020 §5 and §6). What arrives from the file module and the notification module
// arrives as two small ports this module declares (contracts.BundleSource,
// contracts.Notifier) and the composition implements, so nothing here imports a storage
// SDK or a mail client.
package subject

import (
	"time"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/subject/contracts"
	"github.com/septagon-oss/platformkit/modules/subject/internal"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Deps is what this module cannot make for itself: a struct rather than a parameter
// list, so adding one is a named field at every call site. Every module follows this.
type Deps struct {
	// Service is the commands, for a composition that built them for its own reasons and
	// hands the same value to its other modules; nil builds one from the fields below. It
	// has to be the value this package built, because the worker job calls the walk beside
	// the two public commands — which is checked below and refused at composition.
	Service contracts.Service

	// Parts are the contributions every owner of a person's rows handed over, People
	// the one read across the boundary, Bundles where a bundle's bytes go, and Notes
	// how the person hears about them. Tenants is how the export walk reaches active
	// tenants — the same lister modules/task's sweep is handed.
	Parts   []personaldata.Part
	People  usercontracts.Service
	Bundles contracts.BundleSource
	Notes   contracts.Notifier
	Tenants jobs.TenantLister

	// Files is the file module: where a bundle's bytes are stored and the only way to
	// stream one back out of the public door. It is a module contract rather than a
	// port this module wrote because the route hands the caller's own transaction to
	// Open and reads the answer as a stream, which is the file module's whole shape.
	Files filecontracts.Service

	// Now is the clock the bundle's manifest, the receipt and the link's expiry are
	// stamped on; nil means db.Now.
	Now func() time.Time

	// WalkEvery is how often the export walk runs; zero means a minute. A person who
	// asked for a copy is not waiting on the minute, and a faster tick is a query per
	// tenant per tick for nobody's benefit. A test sets it lower.
	WalkEvery time.Duration
}

// walkEvery is the default interval. See Deps.WalkEvery.
const walkEvery = time.Minute

// permissions is what the manifest declares, and kit/app checks every route against it
// at boot, so a route guarded by a key that is not here fails startup rather than denying
// everybody forever. Two keys, matching ADR 0020 §7's table: the operator's export door
// and the erasure door. The person's own export asks for neither, which is why there is no
// third key here to grant.
var permissions = []module.Permission{
	{Key: contracts.PermissionExport, Label: "export a person's data"},
	{Key: contracts.PermissionErase, Label: "erase a person's data"},
}

// roles names the one person this capability is written for: the officer who answers a
// person's request on the tenant's behalf. It grants only the two keys above, which is
// what lets a test ask whether a granted role reaches the doors this module mounts — and
// it is a declaration rather than a composition line because the shape of the grant is the
// module's fact. *Which* of a product's roles hold `subject:erase` is the product's
// SeedRoles line and its responsibility (ADR 0020, Limits).
var roles = []module.RoleDecl{
	{Name: "subject_officer", Grants: []string{contracts.PermissionExport, contracts.PermissionErase}},
}

// Module is the manifest.
//
// It owns two tables and files what they say about a person (`subject_id` is the person
// the request is about, `requested_by` whoever asked, `bundle_file_id` a row and not a
// person), which is what makes the boot rule check the claim against the SQL below. What
// it contributes to *other* people's erasures is nothing: the request rows name the
// requester as well as the subject, and the row that records an erasure has to outlive it,
// which is the opposite of a row an erasure should scrub. The README says so where a
// reader will look for it.
// New is the manifest as this module declares it, with nothing composed: what a reader
// asks, and what a test that wants to know whether the doors exist asks. It is the same
// value NewWith builds from an empty Deps, which is why it is a call and not a copy — a
// second literal would be a second answer that could drift from the one the application
// mounts. A composition hands Deps to NewWith.
func New() module.Module { return NewWith(Deps{}) }

// NewWith is the manifest over the contributions and the ports the composition collected.
func NewWith(deps Deps) module.Module {
	// One value serves the routes and the job, because a module with two answers to
	// "who are the parts" is two modules.
	svc := deps.Service
	if svc == nil {
		svc = internal.NewService(internal.Deps{
			Parts: deps.Parts, People: deps.People, Now: deps.Now,
			Bundles: deps.Bundles, Notes: deps.Notes,
		})
	}
	walker, ok := svc.(internal.Walker)
	if !ok {
		panic("subject: the export walk runs the service this module builds; a foreign one has no queue to walk")
	}
	every := deps.WalkEvery
	if every == 0 {
		every = walkEvery
	}
	return module.Module{
		Name:       "subject",
		Migrations: Migrations.Files,
		Adopts:     Migrations.Adopts,
		PersonalData: personaldata.Implements(
			personaldata.Table{
				Name: "subject_requests",
				// The person the request is about, and whoever asked. The text is
				// their own words — a reason for an export, the address repeated
				// for an erasure — so it is filed as text a scrub runs over.
				Person:    "subject_id",
				About:     []string{"requested_by"},
				Text:      []string{"reason", "detail"},
				NotPerson: []string{"bundle_file_id"},
			},
			personaldata.Table{
				Name: "subject_grants",
				// A link names a request and nobody: the token is hashed and the
				// person is reached only through the row that minted it.
				NotPerson: []string{"request_id"},
			},
		),
		Permissions: permissions,
		Roles:       roles,
		Declared: []events.Declared{
			// One event, and it is the erasure's: an irreversible act has to leave
			// the record that it happened, and modules/audit records every event the
			// application emits, so the trail row commits with the rows.
			events.Declare[contracts.Erased](contracts.EventErased),
			// The two the door publishes: that a person asked, which is what puts
			// the bundle in the worker's queue, and that it is ready, which is what
			// a deployment that watches rather than polls can act on.
			events.Declare[contracts.ExportRequested](contracts.EventExportRequested),
			events.Declare[contracts.ExportReady](contracts.EventExportReady),
		},
		// The work an event cannot express: a bundle is a walk over nine owners, not a
		// reaction to something that happened.
		Jobs: []jobs.Job{internal.ExportWalk(deps.Tenants, walker, every)},
		Routes: func(s httpx.Surfaces) {
			internal.RegisterRoutes(s, svc, deps.Files)
		},
	}
}
