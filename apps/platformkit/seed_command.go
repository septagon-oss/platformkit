package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/seed"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
)

// seedCommand applies this application's embedded seed to one tenant.
//
// Two flags carry the whole authority story. `--tenant` names which tenant, and
// nothing else about it: the tenant's own row is what decides whether demo
// records are its own, so an operator cannot widen a run by asking. `--as` names
// the person the writes belong to — an address the user module resolves inside
// that tenant's transaction — and the seed's authorizer asks the auth module
// whether that person's roles hold each resource's permission, for every record.
// A run without one of the two refuses; there is no mode in which the command
// seeds as nobody.
//
// `--dry-run` prints the plan and writes nothing, and prints the same text a run
// that writes would print: the decision is the same code either way, so a dry run
// that lied would be caught by the run it predicted. In both cases the plan is
// printed only after the transaction has committed, because a plan printed and
// then rolled back is a claim about rows that do not exist.
func seedCommand(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	path := fs.String("config", "config.yaml", "Path to the configuration file")
	slug := fs.String("tenant", "", "Slug of the tenant to seed")
	as := fs.String("as", "", "Address of the person this run writes as")
	demo := fs.Bool("demo", false, "Include the demo records, for a tenant whose own row says it is a demo one")
	dry := fs.Bool("dry-run", false, "Print what this run would write and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for _, required := range []struct{ flag, value string }{
		{"--tenant", *slug}, {"--as", *as},
	} {
		if required.value == "" {
			return fmt.Errorf("seed: %s is required", required.flag)
		}
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	logger(cfg.Log.Level)
	c := compose(cfg)
	service, err := seedService(c)
	if err != nil {
		return err
	}

	var plan seed.Plan
	err = app.RunCommand(context.Background(), cfg, func(ctx context.Context, system db.Tx[db.System]) error {
		// One trace for the run, minted here because a command line inherits no
		// request to inherit one from. Every owner event the run causes carries
		// it into the outbox (kit/events reads it there), which is what lets a
		// later reader join twenty rows of writes and one plan print-out back to
		// one invocation. docs/seed.md's audit contract asks for the same id.
		if _, ok := trace.From(ctx); !ok {
			ctx = trace.With(ctx, trace.New())
		}
		tenants, err := c.tenants.List(ctx, system)
		if err != nil {
			return err
		}
		var target *tenancy.Tenant
		for _, t := range tenants {
			if t.Slug == *slug {
				found := t.Tenancy()
				target = &found
				break
			}
		}
		if target == nil {
			return fmt.Errorf("seed: no tenant %s in this installation", *slug)
		}
		return db.InTenant(ctx, system, *target, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			ctx, err = seedActor(ctx, c.users, tx, *as)
			if err != nil {
				return err
			}
			selection := seed.Selection{Demo: *demo}
			if *dry {
				plan, err = service.Plan(ctx, tx, selection)
			} else {
				plan, err = service.Apply(ctx, tx, selection)
			}
			return err
		})
	})
	if err != nil {
		return err
	}
	fmt.Print(plan.String())
	return nil
}
