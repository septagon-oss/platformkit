package main

// migrate.go is the fourth command, and the shortest: apply what is pending and
// exit.
//
// go run github.com/septagon-oss/platformkit/apps/platformkit@main migrate \
//     --config config.yaml
//
// It exists because contention is not failure. A migration that could not take a
// lock within its budget returns db.ErrContended and says the operator may run it
// again; the runner does not wait and retry inside itself, because it holds the
// composition's advisory lock and a queue inside that lock stops every other
// replica's boot behind the same wait. So the waiting and the running-again is a
// human decision, and the person making it is not deploying — they are standing in
// front of a database whose table somebody is reading. A door for that decision has
// to exist that does not require starting a server.
//
// It is also the step scripts/rehearse_migrations.sh drives a candidate through: a
// rehearsal that applied the migrations by some other route would be rehearsing a
// different program from the one the release ships.

import (
	"context"
	"flag"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
)

// migrate is every role's boot migration over the same composition, sources,
// floors and budgets (kit/app.Migrate is that composition once), without the
// server around it. What applied and how long each file took is in the runner's
// log lines; the exit status says only whether the run as a whole succeeded, and a
// contended file's message is the sentence that says it may be run again.
//
// --drain is the rest of the convergence in one command: the migration, then the
// backfill the migration deliberately left to the worker, then the migration again
// for the files that waited behind it. A deployment converges the same way across
// two ticks of the worker; this is the same three steps for an operator standing in
// front of the database, and the shape scripts/rehearse_migrations.sh runs.
func migrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	path := fs.String("config", "config.yaml", "Path to the configuration file")
	drain := fs.Bool("drain", false, "Finish any backfill the migration left half-done, then migrate again")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	logger(cfg.Log.Level)

	ctx := context.Background()
	modules := compose(cfg).modules
	if err := app.Migrate(ctx, cfg, modules); err != nil {
		return err
	}
	if !*drain {
		return nil
	}
	if err := app.Drain(ctx, cfg, modules); err != nil {
		return err
	}
	return app.Migrate(ctx, cfg, modules)
}
