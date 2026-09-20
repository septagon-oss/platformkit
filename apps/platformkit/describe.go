// describe.go is the one command that answers "how is this application
// composed?" without reading the composition.
//
// It composes exactly what `run` composes, runs exactly the boot gates `run`
// runs, and writes what came out on stdout in the format --format names: the
// description as JSON, or the AsyncAPI and Backstage documents projected from
// it. Nothing is served, migrated or scheduled, and no query is issued: the
// connection is opened because httpx.New requires one and every route it records
// would open a tenant transaction on it. Kernel logging goes to stderr, so a pipe
// receives the document and nothing else.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
)

// formats are the readings describe can print, named here so the flag refuses a
// typo before it has opened a database connection. compositionDocument owns what
// each one is.
var formats = []string{"json", "asyncapi", "backstage"}

func describe(args []string) error {
	fs := flag.NewFlagSet("describe", flag.ContinueOnError)
	path := fs.String("config", "config.yaml", "Path to the configuration file")
	role := fs.String("role", string(app.All), "web, worker, or all")
	format := fs.String("format", "json", "json, asyncapi, or backstage")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !slices.Contains(formats, *format) {
		return fmt.Errorf("describe: --format %q is not one of %s", *format, strings.Join(formats, ", "))
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
	raw, err := compositionDocument(*format, d, cfg.NATS.URL)
	if err != nil {
		return err
	}
	// Exactly one newline at the end of whatever the format wrote: JSON comes back
	// with none and the YAML stream already carries its own. What a redirect
	// captures is then byte for byte the committed copy, so the diff between a
	// change and that file is the change to the composition and nothing else.
	fmt.Fprintln(os.Stdout, strings.TrimSuffix(string(raw), "\n"))
	return nil
}

// compositionDocument is the description in the format the caller named: the
// description itself as JSON, its event half in the dialect an event tool reads,
// or its component graph in the service catalog's format. All three are printed
// and none is served or stored — asking an application how it is composed changes
// nothing about how it is composed.
func compositionDocument(format string, d app.Description, natsURL string) ([]byte, error) {
	switch format {
	case "json":
		return json.MarshalIndent(d, "", "  ")
	case "asyncapi":
		return app.AsyncAPI(d, natsURL)
	case "backstage":
		return app.Backstage(d)
	default:
		return nil, fmt.Errorf("describe: unknown format %q; there are %d", format, len(formats))
	}
}
