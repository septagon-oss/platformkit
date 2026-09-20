// describe.go is the one command that answers "how is this application
// composed?" without reading the composition.
//
// It composes exactly what `run` composes, runs exactly the boot gates `run`
// runs, and writes what came out as JSON on stdout. Nothing is served, migrated
// or scheduled, and no query is issued: the connection is opened because
// httpx.New requires one and every route it records would open a tenant
// transaction on it. Kernel logging goes to stderr, so a pipe receives the
// document and nothing else.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
)

func describe(args []string) error {
	fs := flag.NewFlagSet("describe", flag.ContinueOnError)
	path := fs.String("config", "config.yaml", "Path to the configuration file")
	role := fs.String("role", string(app.All), "web, worker, or all")
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
	a, err := app.New(ctx, cfg, c.modules, appOptions(c, app.Role(*role)))
	if err != nil {
		return err
	}
	conn, err := db.Open(ctx, cfg.Database.URL)
	if err != nil {
		return err
	}
	defer conn.Close()

	d, err := a.Describe(ctx, conn)
	if err != nil {
		return err
	}
	// Struct field order, two spaces, a trailing newline: the same bytes
	// apps/platformkit/testdata/composition.json carries, so the diff between a
	// change and that file is the change to the composition and nothing else.
	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(out))
	return nil
}
