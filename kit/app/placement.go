package app

// placement.go is the standing half of naming an app: the walk that puts an
// existing tenant under the app that proves it, asked of every boot that declares
// one rather than of the one release that ships the migration file. It places whom
// it can prove and never refuses: a tenant it cannot place stays where it is and
// the fact is printed. Why the walk is a deployment step and not a migration file,
// what it deliberately does not inherit from 000060, and why two boots placing
// over one database are safe for each other are in README.md, section "Placing a
// tenant under an app".

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
