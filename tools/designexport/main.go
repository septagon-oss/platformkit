// Command designexport writes the current Core design snapshot to stdout.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	if len(args) != 0 {
		return fmt.Errorf("designexport: takes no arguments")
	}
	snapshot, err := ui.Export(design.Default(), components.Gallery())
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
