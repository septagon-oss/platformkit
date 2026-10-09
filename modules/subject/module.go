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
// What it deliberately does not own yet is the door. There is no Spec, no route, no
// permission and no request row here: the export job, the stored bundle, the
// seven-day link and the `subject:export` and `subject:erase` gates are ADR 0020 §5
// to §7, and this repository refuses a permission nothing checks and a state field
// nothing reads rather than carrying them as promises. The two commands are here
// and composed today; whoever builds the door guards these, because these are the
// only two operations there are.
package subject

import (
	"time"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/subject/contracts"
	"github.com/septagon-oss/platformkit/modules/subject/internal"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// NewService builds the two commands over the contributions this installation
// collected: the parts every owner handed over, the person's own service — the one
// read across the boundary, and the only one — and the clock the bundle's manifest
// and the receipt are stamped on, where nil means db.Now. A composition that wants
// the value before the manifest calls this; provider.go is the one that does.
func NewService(parts []personaldata.Part, people usercontracts.Service, now func() time.Time) contracts.Service {
	return internal.NewService(parts, people, now)
}

// Module is the manifest, and it is small because the module owns no rows: no
// Migrations, so no personal-data claim either — the boot rule asks a module what
// its own tables say about a person, and this one creates none. No Permissions,
// because a key no route guards is a box an administrator can tick forever with no
// effect. No Jobs, because with no request rows there is nothing to walk. No Nav, no
// Spec and no internal/ui, because there is no screen until the door that needs one
// is built, and the page follows the door rather than preceding it.
//
// It takes nothing, and that is the honest signature: the service the resolver
// hands out is built by NewService, in provider.go's wire, and a manifest that
// accepted the parts in order to ignore them would be a claim nobody could check by
// reading it.
func New() module.Module {
	return module.Module{
		Name: "subject",
		Declared: []events.Declared{
			// One event, and it is the erasure's: an irreversible act has to leave
			// the record that it happened, and modules/audit records every event the
			// application emits, so the trail row commits with the rows.
			events.Declare[contracts.Erased](contracts.EventErased),
		},
	}
}
