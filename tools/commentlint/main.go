// Command commentlint reads the comments of every production Go file under a
// root through go/parser and refuses the ones that cite history: a review, a
// round, a finding, a task id or a test file's name. A comment says what the
// code is and why; where the rule came from is in git.
//
// Usage:
//
//	go run ./tools/commentlint [--json] [root]
//
// Exit 0 when nothing is refused, 1 when a comment is refused, 2 when a file
// does not parse. Blocks of 15 or more comment lines and the comment-to-code
// ratio per package are reported as a trend and never fail the run. One
// reviewed comment may keep a citation with `//lint:comment-history <reason>`
// on the line above it; the reason is printed on every run.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("commentlint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	rep, err := Scan(root)
	if err != nil {
		fmt.Fprintln(stderr, "commentlint:", err)
		return 2
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
	} else {
		fmt.Fprint(stdout, rep.Text())
	}
	if len(rep.Refused) > 0 {
		return 1
	}
	return 0
}
