// Command complexity measures source debt and enforces reviewed ceilings.
// See README.md for the measurement and history contracts.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type options struct {
	root, base, mergeBase string
	check, historyOnly    bool
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func parseOptions(args []string, stderr io.Writer) (options, error) {
	var o options
	flags := flag.NewFlagSet("complexity", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&o.root, "root", ".", "Git worktree root")
	flags.StringVar(&o.base, "base", "", "exact full ancestor commit SHA")
	flags.StringVar(&o.mergeBase, "merge-base", "", "reference whose merge base with HEAD is checked")
	flags.BoolVar(&o.check, "check", false, "require exact ceilings and valid budget history")
	flags.BoolVar(&o.historyOnly, "history-only", false, "validate budget history without current count equality")
	if err := flags.Parse(args); err != nil {
		return o, err
	}
	invalidBase := false
	flags.Visit(func(f *flag.Flag) {
		if (f.Name == "base" || f.Name == "merge-base") && f.Value.String() == "" {
			invalidBase = true
		}
	})
	checking := o.check || o.historyOnly
	if invalidBase || flags.NArg() != 0 || (o.check && o.historyOnly) || (o.base != "" && o.mergeBase != "") || (checking != (o.base != "" || o.mergeBase != "")) {
		return o, fmt.Errorf("use report mode without a base, or exactly one of --check/--history-only and one of --base/--merge-base")
	}
	return o, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	o, err := parseOptions(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err == nil {
		err = execute(o, stdout)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		if _, ok := errors.AsType[refusal](err); ok {
			return 1
		}
		return 2
	}
	return 0
}

type refusal struct{ problems []string }

func (r refusal) Error() string { return strings.Join(r.problems, "\n") }

func execute(o options, out io.Writer) error {
	r, err := openRepository(o.root)
	if err != nil {
		return err
	}
	var h *history
	var ceilings budget
	var problems []string
	if o.check || o.historyOnly {
		h, err = startHistory(r, o.base, o.mergeBase)
		if err != nil {
			return err
		}
		raw, err := readSource(r.root, budgetFile)
		if err != nil {
			return err
		}
		ceilings, err = decodeBudget(raw)
		if err != nil {
			return err
		}
		problems, err = h.check(ceilings)
		if err != nil {
			return err
		}
	}
	var m measurement
	if !o.historyOnly {
		files, err := r.sources("")
		if err != nil {
			return err
		}
		m, err = measure(files)
		if err != nil {
			return err
		}
		if o.check {
			problems = append(problems, countProblems(m.counts, ceilings)...)
		}
	}
	if h != nil {
		if err := h.stable(); err != nil {
			return err
		}
		fmt.Fprintf(out, "HEAD %s\nBASE %s\n", h.head, h.base)
	}
	if !o.historyOnly {
		printMeasurement(out, m, ceilings)
	}
	if len(problems) != 0 {
		return refusal{problems}
	}
	if h != nil {
		fmt.Fprintln(out, "complexity OK")
	}
	return nil
}

func printMeasurement(out io.Writer, m measurement, ceilings budget) {
	for _, key := range measures {
		fmt.Fprintf(out, "%s %d", key, m.counts[key])
		if ceilings != nil {
			fmt.Fprintf(out, " / %d", ceilings[key])
		}
		fmt.Fprintln(out)
		if ceilings == nil || m.counts[key] <= ceilings[key] {
			continue
		}
		for _, offender := range m.offenders {
			if offender.measure != key {
				continue
			}
			unit := "declarations"
			switch key {
			case "funcs_over_100", "files_over_800", "long_comment_blocks", "history_comment_lines":
				unit = "lines"
			case "cc_over_30":
				unit = "complexity"
			}
			fmt.Fprintf(out, "  %s:%d %s size=%d %s\n", escaped(offender.path), offender.line, escaped(offender.name), offender.size, unit)
		}
	}
}

func escaped(s string) string {
	quoted := strconv.Quote(s)
	return quoted[1 : len(quoted)-1]
}
