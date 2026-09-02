package internal

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// sweepCron is hourly. Both things this job does are measured in days — a
// session lasts thirty of them and a token an hour — so an hour of lag on
// forgetting is free, and a credential row that outlives its own expiry is not
// a live credential: every lookup checks the clock before it checks anything
// else. What the sweep buys is that the table stops growing and a dump of it
// stops being a history of who signed in from where.
const sweepCron = "0 * * * *"

// Sweep is this module's periodic work: expired sessions and spent tokens go,
// and a role naming a permission no module defines is said out loud.
//
// It is a job and not a subscription because nothing happens when a session
// expires — the clock passes, which is the distinction docs/adr/0004 draws.
//
// The warning half is here rather than at boot, and that is a deliberate second
// choice. A role can only come to name an undeclared permission one way: a
// module left the composition, taking its permissions with it, because SetRole
// refuses to write one. So the moment worth reporting is a deploy, and this
// reports it within the hour of one. Doing it at boot would mean either a
// kernel that reads a module's table or a "run this at startup" field on the
// manifest that every module would find a use for, and neither is worth an hour.
func Sweep(svc *Service, tenants jobs.TenantLister) jobs.Job {
	return jobs.Job{
		Name: "auth-sweep",
		Cron: sweepCron,
		Run: func(ctx context.Context, conn *db.Conn) error {
			return jobs.PerTenant(ctx, conn, tenants, func(ctx context.Context, conn *db.Conn, t tenancy.Tenant) error {
				if err := svc.purge(ctx, conn, t); err != nil {
					return err
				}
				svc.warn(ctx, conn, t)
				return nil
			})
		},
	}
}

// purge deletes this tenant's expired credentials, a transaction per batch, so
// that a tenant with a million dead sessions is a thousand short transactions
// rather than one long lock — and a worker asked to stop half way has already
// committed what it deleted.
func (s *Service) purge(ctx context.Context, conn *db.Conn, t tenancy.Tenant) error {
	for {
		var gone int64
		err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var err error
			gone, err = s.Purge(ctx, tx)
			return err
		})
		if err != nil {
			return fmt.Errorf("auth: sweep %s: %w", t.Slug, err)
		}
		if gone < int64(purgeBatch) {
			return nil
		}
	}
}

// warn says which of this tenant's roles name a permission nothing defines.
//
// It is a log line and not a failure: the rows belong to a customer and were
// legal when they were written, so a sweep that refused would turn dropping a
// module into an installation somebody has to repair by hand. What it buys is
// that "this role grants nothing and nobody can see why" is a line in the log
// of the deploy that caused it rather than a support conversation months later.
func (s *Service) warn(ctx context.Context, conn *db.Conn, t tenancy.Tenant) {
	declared := s.declared()
	if len(declared) == 0 {
		// Nothing was declared, which means Routes never ran — a composition
		// with no HTTP surface at all. Reporting every permission as undeclared
		// would be noise about a list nobody filled in.
		return
	}
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		roles, err := s.Roles(ctx, tx)
		if err != nil {
			return err
		}
		for name, unknown := range Undeclared(roles, declared) {
			slog.WarnContext(ctx, "auth: a role names permissions no module defines, so they grant nothing",
				"tenant", t.Slug, "role", name, "permissions", unknown)
		}
		return nil
	})
	if err != nil {
		slog.ErrorContext(ctx, "auth: could not check the roles of a tenant", "tenant", t.Slug, "error", err)
	}
}
