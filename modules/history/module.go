// Package history is the module manifest: every version of a record's content,
// kept by the write that made it, compared and restored without rewriting anything.
package history

import (
	"context"
	"errors"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/history/contracts"
	"github.com/septagon-oss/platformkit/modules/history/internal"
)

// errNoService is the adapter's answer when it was built without the store it
// adapts: a wiring mistake, said once, rather than a version that silently vanished.
var errNoService = errors.New("history: no store to keep the version with")

// Deps is what this module cannot make for itself: the ledger a version's pictures
// are filed in, the list the retention sweep finds tenants on, and the rule it keeps.
type Deps struct {
	Uses    rest.FileUses
	Tenants jobs.TenantLister
	// Retention is how much history stands. Zero is contracts.Defaults — ninety
	// days or the newest fifty versions, whichever keeps more — because those are
	// the numbers this module can name; how much a client's own authors need is the
	// client's sentence in its composition, and no sector's answer is written here.
	Retention contracts.Rule
}

// New builds the store, the port a mounted resource records through, and the
// manifest. The rule is refused at boot when it keeps nothing that could be
// restored, which is the only answer that leaves the versions standing.
func New(deps Deps) (contracts.Service, httpx.Versions, module.Module, error) {
	if bad := deps.Retention.Fault(); bad != "" {
		return nil, nil, module.Module{}, errors.New(bad)
	}
	svc := internal.NewService(deps.Uses)
	versions := KeepVersions{Service: svc}
	return svc, versions, module.Module{
		Name:       "history",
		Migrations: Migrations.Files,
		// No permissions and no Nav: this module owns no screen and mounts no route
		// of its own. Its versions are read at the record's own address, under the
		// record's own read permission, and a permission with nothing behind it is
		// a grant that does nothing.
		Permissions:   nil,
		Nav:           nil,
		Routes:        nil,
		Declared:      contracts.Events,
		Subscriptions: nil,
		Jobs:          []jobs.Job{retention(svc, deps.Tenants, deps.Retention)},
	}, nil
}

// retention is the sweep: a series keeps the versions its rule keeps, and the rest
// go, hourly, one tenant at a time, inside each tenant's own transaction so that
// row-level security is the boundary inside the job exactly as it is inside a
// request. Nothing is published: an expiry that announced itself would be relayed
// into the table the relay is trimming.
func retention(svc *internal.Service, tenants jobs.TenantLister, rule contracts.Rule) jobs.Job {
	return jobs.Job{
		Name: "history-retention",
		Cron: "0 * * * *",
		Run: func(ctx context.Context, app *db.Conn) error {
			if tenants == nil {
				return errors.New("history: nothing to retain: no tenant lister is wired, " +
					"so versions would stand forever and nobody would know whether that was the plan")
			}
			return jobs.PerTenant(ctx, app, tenants, func(ctx context.Context, conn *db.Conn, t tenancy.Tenant) error {
				return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					_, err := svc.Prune(ctx, tx, rule, db.Now())
					return err
				})
			})
		},
	}
}
