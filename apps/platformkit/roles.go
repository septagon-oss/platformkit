package main

// roles.go is the fifth command, and it exists for installations seeded before
// a grant was only ever as wide as the composition:
//
//	platformkit repair-roles --config config.yaml           # say what is there
//	platformkit repair-roles --config config.yaml --remove  # take it away
//
// The administrator's role used to be seeded with a list of operator
// permissions written out by hand, so a product that dropped the module owning
// one kept the grant and warned about it on the hour, forever. Nothing seeds
// one now; the rows already written are a customer's, and removing them is a
// person's decision, made by reading this first.

import (
	"context"
	"flag"
	"fmt"
	"maps"
	"slices"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// repairRoles walks every tenant and reports the grants its own seeder wrote
// that no composed module defines any more. --remove takes them away, one
// transaction per tenant, through the module's ordinary role write: the tenant's
// lock, the floor that keeps somebody able to administer it, and an auth.role_set
// event in the same transaction, so the repair is in the audit like any other
// change to a role.
//
// The two values it passes are the two the seeder is given — the catalogue of
// this composition and the initial roles this application names — because those
// two are what the seeder decided with, and a grant it did not write is not this
// command's to take away. A grant somebody added by hand is reported by the
// hourly sweep and by nothing here. It is idempotent: a second run finds nothing,
// and prints that.
func repairRoles(args []string) error {
	fs := flag.NewFlagSet("repair-roles", flag.ContinueOnError)
	path := fs.String("config", "config.yaml", "Path to the configuration file")
	remove := fs.Bool("remove", false, "Remove the grants found; without it they are only listed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	logger(cfg.Log.Level)

	ctx := context.Background()
	c := compose(cfg)
	declared := module.Grants(c.modules)
	conn, err := db.Open(ctx, cfg.Database.URL)
	if err != nil {
		return err
	}
	defer conn.Close()

	var found int
	err = jobs.PerTenant(ctx, conn, tenantcontracts.Active{Service: c.tenants},
		func(ctx context.Context, conn *db.Conn, t tenancy.Tenant) error {
			return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				stale, err := auth.RepairSeededRoles(ctx, tx, c.auth, declared, initialRoles, *remove)
				if err != nil {
					return err
				}
				for _, name := range slices.Sorted(maps.Keys(stale)) {
					found++
					verb := "seeded grants no composed module defines"
					if *remove {
						verb = "removed"
					}
					fmt.Printf("%s\t%s\t%s\t%v\n", t.Slug, name, verb, stale[name])
				}
				return nil
			})
		})
	if err != nil {
		return err
	}
	if found == 0 {
		fmt.Println("every grant this installation's seeder wrote is one a composed module defines")
	}
	return nil
}
