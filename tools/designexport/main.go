// Command designexport projects Core's existing typed examples to stdout.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, input io.Reader, output io.Writer) error {
	examples, err := projectExamples(args, input)
	if err != nil {
		return err
	}
	snapshot, err := ui.Export(design.Default(), examples)
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

const maxPropsBytes = 1 << 20

// The CLI selects an existing invocation, then delegates property semantics and
// rendering to that invocation. It never constructs nodes from JSON or edits Go.
func projectExamples(args []string, input io.Reader) ([]components.Example, error) {
	examples := components.Gallery()
	if len(args) == 0 {
		return examples, nil
	}
	if (len(args) != 2 && len(args) != 3) || args[0] != "--example" || args[1] == "" || (len(args) == 3 && args[2] != "--props") {
		return nil, fmt.Errorf("designexport: usage: designexport [--example ID [--props]]; --props reads one JSON object from stdin")
	}
	index := slices.IndexFunc(examples, func(example components.Example) bool { return example.ID == args[1] })
	if index < 0 {
		return nil, fmt.Errorf("designexport: unknown example %q", args[1])
	}
	example := examples[index]
	if len(args) == 3 {
		patch, err := io.ReadAll(io.LimitReader(input, maxPropsBytes+1))
		if err != nil {
			return nil, fmt.Errorf("designexport: read props: %w", err)
		}
		if len(patch) > maxPropsBytes {
			return nil, fmt.Errorf("designexport: props exceed %d bytes", maxPropsBytes)
		}
		example, err = example.WithProps(patch)
		if err != nil {
			return nil, fmt.Errorf("designexport: project example %q: %w", args[1], err)
		}
	}
	return []components.Example{example}, nil
}
