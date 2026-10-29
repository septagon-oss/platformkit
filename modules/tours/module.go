// Package tours provides tenant-scoped tour runs and their transition events.
package tours

import (
	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tours/contracts"
)

// Deps contains the current policy, UTC clock and app namespace used by runs.
type Deps struct {
	Clock   contracts.Clock
	Policy  tenancy.Policy
	App     appname.Name
	Service contracts.Service
}

// New returns the manifest for the tour runtime and its typed event promises.
func New(deps Deps) module.Module {
	_ = deps.Service
	return module.Module{
		Name:       "tours",
		Migrations: Migrations.Files,
		Permissions: []module.Permission{
			{Key: contracts.PermissionRead, Label: "read tour runs"},
			{Key: contracts.PermissionRun, Label: "play tours"},
		},
		Declared: []events.Declared{
			events.Declare[contracts.Event](contracts.EventStarted),
			events.Declare[contracts.Event](contracts.EventStepViewed),
			events.Declare[contracts.Event](contracts.EventCompleted),
			events.Declare[contracts.Event](contracts.EventDismissed),
		},
	}
}
