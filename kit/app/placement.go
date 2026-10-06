package app

// placement.go is the standing half of naming an app: the walk that puts an
// existing tenant under the app that proves it, asked of every boot that declares
// one rather than of the one release that ships the migration file.
//
// The file (migrations/000046_tenant_app_place.up.sql) and this step are the same
// walk — `platformkit_place_tenants`, created by 000045 — and they are two because
// a migration file is spent the moment it drains. The upgrade that installs this
// release while the deployment still names no app is a supported boot; it drains
// 000046 over rows it must not touch, history marks the file applied, and the
// operator's later `nats.app` arrives to a ledger that says the placement already
// happened. Without this step those tenants keep the empty app they were stamped
// with: served, on the legacy durable, outside both named apps, and RelayApp finds
// nothing for the composition that has just named itself after them
// (kit/events/relay.go). Choosing an app name is a deployment decision made after
// the release is installed, so the placement has to be a deployment step.
//
// What it does not inherit is the refusal. migrations/000046 refuses a boot that
// named itself and left a tenant nobody named, and that is right at the release,
// where one statement sees every row and the operator is mid-deploy. It would be
// the wrong answer here: a tenant created after the placement ran is a tenant whose
// app its next deploy's declaration may simply not name yet, and a boot that died on
// it would be the write that takes the last one away, held over a boot that came to
// place somebody. So the walk runs, the tenants it cannot prove stay where they are,
// and the fact is printed rather than decided.
//
// Two of these run at once, and that is the shape of a server holding two apps over
// one database: each composition places on its own boot and on its own tick, over the
// same rows, with no knowledge of the other. The walk decides from a reading, so the
// guarantee lives in the write it makes: `platformkit_place_tenants` sets the app of a
// row only while that row is still empty at the write itself (migrations/000045), which
// leaves the second of two competing placements to find the row already named and write
// nothing. A tenant's app is decided once (migrations/000043) and which relay, consumer
// and control-plane scope can reach the tenant follows from it, so the step from empty
// to a slug is the one write in this kernel that may not arrive twice; a declaration
// that finds a row already answered does not move the tenant and does not report it as
// somebody's omission either — the late declaration is the one that is wrong about the
// row, not the row about itself.
//
// The step also declares the tenant's ledger key, in the schema rather than here
// (migrations/000047's trigger on the row whose app went from nobody's to somebody's),
// and that is what makes two boots safe for each other's *ledgers* rather than merely
// consistent about one row. kit/events.MoveLedger holds that key exclusively over every
// tenant that could still become its app: a placement that arrives first is that move's
// ordinary contention refusal, and one that arrives after waits and lands behind it,
// which is the order that owes a move — and this step runs before a boot opens any
// scoped consumer, so its own next move pays the debt. What it costs is a placement
// sitting behind another app's rename: one short transaction, the only holder it can ever
// wait behind, cheaper than the tenant whose claims were already committed when that move
// read the ledger and answered that nothing was left to move.

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
)

// placementToken is the capability of the placement write: `tenants` carries a row
// per tenant and the placement has no tenant transaction to be asked from — the
// boot that names an app answers for every tenant its declaration proves, which is
// the definition of a cross-tenant write, and this is the door the kernel's other
// cross-tenant writes use (kit/app/bootstrap.go, kit/events/ledger.go).
var placementToken = syscap.NewSystemToken("place a tenant under the app its boot's declaration proves")

// placeTenants runs the placement over the whole table for one boot's declaration.
//
// It is called by Migrate and by Drain — the two doors a deployment walks when its
// configuration changed — and it does nothing at all when this boot declares nobody:
// a deployment with no slug and no mapping has asserted nothing about whose anybody's
// tenant is, which is the same reading migrations/00046's guard makes of the same
// session, and the two doors cannot then tell different stories about one row.
func placeTenants(ctx context.Context, cfg config.Config) error {
	decl, err := placementDeclaration(cfg)
	if err != nil {
		return err
	}
	if decl.own == "" && decl.mapping == "" {
		return nil
	}
	// The application role, not the migration role: this is a write of tenant rows
	// through the same door every other cross-tenant kernel write uses
	// (kit/app/bootstrap.go), and kit/db refuses a superuser connection precisely
	// because it would make the row-level security below not apply.
	conn, err := db.Open(ctx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("app: place tenants: open: %w", err)
	}
	defer conn.Close()
	var unplaced string
	err = db.RunSystem(ctx, conn, placementToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw(
			"SELECT coalesce(platformkit_place_tenants(?, ?, ?, NULL), '')",
			decl.own, decl.hosts, decl.mapping).Scan(&unplaced).Error
	})
	if err != nil {
		return fmt.Errorf("app: place tenants: %w", err)
	}
	if unplaced != "" {
		// Not a refusal, and printed where the operator who is about to name an app
		// will read it: these tenants are still on the legacy durable, and no
		// delivery of theirs will reach the app this boot named.
		slog.WarnContext(ctx, "app: tenants still name no app after the placement",
			"tenants", unplaced,
			"fix", "name each one in app.tenant_apps (or app.tenant_apps_file), or serve its hosts from this composition")
	}
	return nil
}

// placementDeclaration is the boot's answer, in the three values the placement
// function takes. The slug is the same one every shared name is formed from
// (migrationDeclaration), and the empty string means the same thing it means there:
// this deployment named itself nothing.
type placement struct {
	own, hosts, mapping string
}

func placementDeclaration(cfg config.Config) (placement, error) {
	slug, err := cfg.NATS.AppName()
	if err != nil {
		return placement{}, err
	}
	out := placement{own: slug.String(), hosts: strings.Join(cfg.App.Hosts, ",")}
	pairs := make([]string, 0, len(cfg.App.TenantApps))
	for tenant, app := range cfg.App.TenantApps {
		pairs = append(pairs, tenant+"="+app)
	}
	// Sorted, so that one boot's declaration and the next boot's are the same string
	// and the placement's own log line does not move between two identical runs.
	sort.Strings(pairs)
	out.mapping = strings.Join(pairs, ",")
	return out, nil
}
