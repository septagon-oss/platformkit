// Command designexport projects typed examples and verifies Go source edits.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/export"
	"github.com/septagon-oss/platformkit/ui/source"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, input io.Reader, output io.Writer) error {
	if len(args) > 0 && args[0] == "--source" {
		return runSource(args, input, output)
	}
	snapshot, err := projectSnapshot(args, input)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		return fmt.Errorf("designexport: write snapshot: %w", err)
	}
	return nil
}

func runSource(args []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("designexport --source", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var target source.Target
	var producer source.GoProducer
	flags.StringVar(&target.File, "source", "", "owning module Go source file")
	flags.IntVar(&target.Line, "line", 0, "capture call start line")
	flags.IntVar(&target.Column, "column", 0, "capture call start column, if ambiguous")
	flags.StringVar(&target.SHA256, "sha256", "", "current source file SHA-256")
	flags.StringVar(&producer.Dir, "dir", ".", "owning Go module root")
	flags.StringVar(&producer.Package, "producer", "./tools/designexport", "local producer main package")
	apply := flags.Bool("apply", false, "persist the verified candidate")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if target.File == "" || target.Line < 1 || target.Column < 0 || len(target.SHA256) != 64 {
		return fmt.Errorf("designexport: --source requires FILE, --line and --sha256; optional --apply writes source")
	}
	producer.Args = flags.Args()
	body, err := readInput(input)
	if err != nil {
		return err
	}
	var proposal export.PropsProposal
	if err := json.Unmarshal(body, &proposal); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	change, err := source.Prepare(ctx, producer, target, proposal)
	if err != nil {
		return err
	}
	if *apply {
		if err := change.Apply(ctx); err != nil {
			return err
		}
	}
	if err := json.NewEncoder(output).Encode(map[string]any{"change": change.Review(), "applied": *apply}); err != nil {
		return fmt.Errorf("designexport: write source review (applied=%t): %w", *apply, err)
	}
	return nil
}

func projectSnapshot(args []string, input io.Reader) (export.DesignExport, error) {
	if len(args) != 0 && args[0] == "--tokens" {
		if len(args) != 2 || !slices.Contains([]string{"light", "dark", "both"}, args[1]) {
			return export.DesignExport{}, fmt.Errorf("designexport: usage: designexport --tokens light|dark|both")
		}
		modes := []string{args[1]}
		if args[1] == "both" {
			modes = []string{"light", "dark"}
		}
		theme := design.Default()
		tokens, err := export.ExportTokens(theme, modes...)
		if err != nil {
			return export.DesignExport{}, err
		}
		base, err := export.Export(theme, nil)
		if err != nil {
			return export.DesignExport{}, err
		}
		return base.WithTokens(tokens)
	}
	if len(args) == 1 && (args[0] == "--proposal" || args[0] == "--replacement") {
		body, err := readInput(input)
		if err != nil {
			return export.DesignExport{}, err
		}
		if args[0] == "--replacement" {
			var proposal export.ReplacementProposal
			if err := json.Unmarshal(body, &proposal); err != nil {
				return export.DesignExport{}, fmt.Errorf("designexport: read replacement: %w", err)
			}
			_, snapshot, err := export.ProjectReplacement(design.Default(), examples.Gallery(), proposal)
			return snapshot, err
		}
		var proposal export.PropsProposal
		if err := json.Unmarshal(body, &proposal); err != nil {
			return export.DesignExport{}, fmt.Errorf("designexport: read proposal: %w", err)
		}
		_, snapshot, err := export.ProjectProps(design.Default(), examples.Gallery(), proposal)
		return snapshot, err
	}
	captures, err := projectExamples(args, input)
	if err != nil {
		return export.DesignExport{}, err
	}
	return export.Export(design.Default(), captures)
}

const maxInputBytes = 1 << 20

// The CLI selects an existing invocation, then delegates property semantics and
// rendering to that invocation. This projection never constructs nodes from JSON.
func projectExamples(args []string, input io.Reader) ([]examples.Example, error) {
	captures := examples.Gallery()
	if len(args) == 0 {
		return captures, nil
	}
	if (len(args) != 2 && len(args) != 3) || args[0] != "--example" || args[1] == "" || (len(args) == 3 && args[2] != "--props") {
		return nil, fmt.Errorf("designexport: usage: designexport [--example ID [--props] | --proposal | --replacement | --tokens light|dark|both]; edit flags read one JSON object from stdin")
	}
	index := slices.IndexFunc(captures, func(example examples.Example) bool { return example.ID == args[1] })
	if index < 0 {
		return nil, fmt.Errorf("designexport: unknown example %q", args[1])
	}
	example := captures[index]
	if len(args) == 3 {
		patch, err := readInput(input)
		if err != nil {
			return nil, err
		}
		example, err = example.WithProps(patch)
		if err != nil {
			return nil, fmt.Errorf("designexport: project example %q: %w", args[1], err)
		}
	}
	return []examples.Example{example}, nil
}

func readInput(input io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(input, maxInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("designexport: read input: %w", err)
	}
	if len(body) > maxInputBytes {
		return nil, fmt.Errorf("designexport: input exceeds %d bytes", maxInputBytes)
	}
	return body, nil
}
