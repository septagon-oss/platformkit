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

func projectSnapshot(args []string, input io.Reader) (ui.DesignExport, error) {
	if len(args) == 1 && (args[0] == "--proposal" || args[0] == "--replacement") {
		body, err := readInput(input)
		if err != nil {
			return ui.DesignExport{}, err
		}
		if args[0] == "--replacement" {
			var proposal ui.ReplacementProposal
			if err := json.Unmarshal(body, &proposal); err != nil {
				return ui.DesignExport{}, fmt.Errorf("designexport: read replacement: %w", err)
			}
			_, snapshot, err := ui.ProjectReplacement(design.Default(), components.Gallery(), proposal)
			return snapshot, err
		}
		var proposal ui.PropsProposal
		if err := json.Unmarshal(body, &proposal); err != nil {
			return ui.DesignExport{}, fmt.Errorf("designexport: read proposal: %w", err)
		}
		_, snapshot, err := ui.ProjectProps(design.Default(), components.Gallery(), proposal)
		return snapshot, err
	}
	examples, err := projectExamples(args, input)
	if err != nil {
		return ui.DesignExport{}, err
	}
	return ui.Export(design.Default(), examples)
}

const maxInputBytes = 1 << 20

// The CLI selects an existing invocation, then delegates property semantics and
// rendering to that invocation. It never constructs nodes from JSON or edits Go.
func projectExamples(args []string, input io.Reader) ([]components.Example, error) {
	examples := components.Gallery()
	if len(args) == 0 {
		return examples, nil
	}
	if (len(args) != 2 && len(args) != 3) || args[0] != "--example" || args[1] == "" || (len(args) == 3 && args[2] != "--props") {
		return nil, fmt.Errorf("designexport: usage: designexport [--example ID [--props] | --proposal | --replacement]; edit flags read one JSON object from stdin")
	}
	index := slices.IndexFunc(examples, func(example components.Example) bool { return example.ID == args[1] })
	if index < 0 {
		return nil, fmt.Errorf("designexport: unknown example %q", args[1])
	}
	example := examples[index]
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
	return []components.Example{example}, nil
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
