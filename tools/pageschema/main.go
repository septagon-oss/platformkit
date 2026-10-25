// Command pageschema writes the JSON Schema of the platform's Page contract, so
// that a shell in another language builds against the same shape the Go types
// project rather than against a transcription of them.
//
// The projection is kit/events' own subset — the one that writes every event
// payload schema — so the Page document and the AsyncAPI payload documents are
// the same dialect from the same code. It runs offline and reads no clock: two
// runs write identical bytes.
//
// Usage:
//
//	go run ./tools/pageschema -out kit/entity/view/page-v1.schema.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"

	"github.com/septagon-oss/platformkit/kit/entity/view"
	"github.com/septagon-oss/platformkit/kit/events"
)

func main() {
	out := flag.String("out", "kit/entity/view/page-v1.schema.json", "file to write")
	flag.Parse()
	if err := write(*out); err != nil {
		fmt.Fprintln(os.Stderr, "pageschema:", err)
		os.Exit(1)
	}
}

// write projects view.Page and replaces the file whole, so a member that left the
// type leaves the document too. The version is in the file name, which is how a
// consumer pins a revision: a build against page-v1 never silently reads a page-v2.
func write(path string) error {
	raw, err := json.MarshalIndent(events.SchemaOf(reflect.TypeFor[view.Page]()), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}
