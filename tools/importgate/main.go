// Command importgate checks direct Go imports, including test provenance.
// It returns 0 for acceptance, 1 for a policy refusal, and 2 for an incomplete
// reading. --edges measures a valid graph without gating on policy refusals.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, runGo)
	cancel()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, goCommand command) int {
	flags := flag.NewFlagSet("importgate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	measure := flags.Bool("edges", false, "report cross-module direct edges as JSON")
	modules := flags.Bool("modules", false, "report the aggregate module graph and its layers as JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *measure && *modules {
		fmt.Fprintln(stderr, "imports: --edges and --modules report different graphs; name one")
		return 2
	}
	root := "."
	if flags.NArg() > 0 {
		root = flags.Arg(0)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintln(stderr, "imports:", err)
		return 2
	}
	dependencies := flags.Args()
	if len(dependencies) > 0 {
		dependencies = dependencies[1:]
	}
	g, err := load(ctx, root, dependencies, goCommand)
	if err != nil {
		fmt.Fprintln(stderr, "imports:", err)
		return 2
	}
	grants, err := readGrants(root, g)
	if err != nil {
		fmt.Fprintln(stderr, "imports:", err)
		return 2
	}
	baseline, err := readBaseline(root, g)
	if err != nil {
		fmt.Fprintln(stderr, "imports:", err)
		return 2
	}
	layers, err := readLayers(g)
	if err != nil {
		fmt.Fprintln(stderr, "imports:", err)
		return 2
	}
	decision := decide(g, grants, baseline, layers)
	if _, err := io.WriteString(stderr, notices(grants, decision.used)+baselineNotices(baseline, decision.carried)+layers.notices(decision.spent, decision.justified)+diagnostics(decision.refused)); err != nil {
		return 2
	}
	if *measure || *modules {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		aggregate := any(edgeReport(g))
		if *modules {
			aggregate = moduleGraph(g, layers, decision.justified)
		}
		if err := encoder.Encode(aggregate); err != nil {
			fmt.Fprintln(stderr, "imports: write report:", err)
			return 2
		}
		return 0
	}
	if len(decision.refused) > 0 {
		return 1
	}
	accepted := "imports: direct production and test imports satisfy the module boundaries"
	if len(decision.carried) > 0 {
		accepted += "; the contract baseline rows above carry every production contract dependency the strict rule refuses"
	}
	if _, err := fmt.Fprintln(stdout, accepted); err != nil {
		return 2
	}
	return 0
}
