// Command locbudget counts tracked source lines per bucket and fails when a
// bucket exceeds its committed maximum. It only ever ratchets maxima down.
//
// Usage:
//
//	go run ./tools/locbudget --check                  # exit 1 if any bucket is over budget
//	go run ./tools/locbudget --write                  # lower maxima to current counts
//	go run ./tools/locbudget --write --bucket go_prod # lower one bucket only
//	go run ./tools/locbudget --write --round 100      # re-baseline: count, rounded up
//	go run ./tools/locbudget --write --round 100 --allow 500
//	      # re-baseline with one acceptance round reserved first
//	go run ./tools/locbudget                          # print counts
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Bucket selects tracked files by path prefix, directory name, suffix and
// content.
//
// DirSuffixes matches the last element of a file's directory, which in Go is
// its package: "test" selects tasktest and tenanttest and nothing else. That is
// the distinction the budget needs and a path prefix cannot make — a fake and a
// conformance suite live beside the code they are about, they are shipped
// rather than compiled out, and counting them against the production ceiling of
// the module they belong to prices a fake as if it were a feature.
type Bucket struct {
	Name               string   `json:"name"`
	Paths              []string `json:"paths,omitempty"`                // path prefixes; empty means all
	Suffixes           []string `json:"suffixes"`                       // any of these suffixes
	ExcludeSuffixes    []string `json:"exclude_suffixes,omitempty"`     // none of these suffixes
	ExcludePaths       []string `json:"exclude_paths,omitempty"`        // none of these prefixes
	DirSuffixes        []string `json:"dir_suffixes,omitempty"`         // directory name ends in any of these
	ExcludeDirSuffixes []string `json:"exclude_dir_suffixes,omitempty"` // directory name ends in none of these
	Contains           string   `json:"contains,omitempty"`             // file must contain this substring
	Max                int      `json:"max"`
}

// Budget is the committed file.
type Budget struct {
	Buckets []Bucket `json:"buckets"`
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func hasAnySuffix(s string, suffixes []string) bool {
	for _, p := range suffixes {
		if strings.HasSuffix(s, p) {
			return true
		}
	}
	return false
}

func (b Bucket) matches(rel string) bool {
	if len(b.Paths) > 0 && !hasAnyPrefix(rel, b.Paths) {
		return false
	}
	if hasAnyPrefix(rel, b.ExcludePaths) {
		return false
	}
	if !hasAnySuffix(rel, b.Suffixes) {
		return false
	}
	if hasAnySuffix(rel, b.ExcludeSuffixes) {
		return false
	}
	dir := filepath.Base(filepath.Dir(rel))
	if len(b.DirSuffixes) > 0 && !hasAnySuffix(dir, b.DirSuffixes) {
		return false
	}
	if hasAnySuffix(dir, b.ExcludeDirSuffixes) {
		return false
	}
	return true
}

// Count returns the total newline count of matching files under root. It
// counts newlines, so a file with no trailing newline counts one line short;
// that is stable and monotone, which is all the ratchet needs.
func (b Bucket) Count(root string, files []string) (int, error) {
	total := 0
	for _, rel := range files {
		if !b.matches(rel) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			if os.IsNotExist(err) {
				continue // deleted in working tree but still in index
			}
			return 0, err
		}
		if b.Contains != "" && !bytes.Contains(data, []byte(b.Contains)) {
			continue
		}
		total += bytes.Count(data, []byte{'\n'})
	}
	return total, nil
}

// Has reports whether the budget defines a bucket with this name.
func (bu Budget) Has(name string) bool {
	for _, b := range bu.Buckets {
		if b.Name == name {
			return true
		}
	}
	return false
}

// Validate rejects budgets that cannot measure what they claim to. A bucket
// with no suffixes matches no file, so it would ratchet to 0 and stay green
// forever; duplicate names make counts and ratchets ambiguous.
func (bu Budget) Validate() error {
	seen := make(map[string]bool, len(bu.Buckets))
	for _, b := range bu.Buckets {
		if b.Name == "" {
			return fmt.Errorf("bucket with empty name")
		}
		if seen[b.Name] {
			return fmt.Errorf("duplicate bucket %q", b.Name)
		}
		seen[b.Name] = true
		if len(b.Suffixes) == 0 {
			return fmt.Errorf("bucket %q has no suffixes, so it can never match a file", b.Name)
		}
	}
	return nil
}

// Check returns one message per bucket over its maximum.
func (bu Budget) Check(counts map[string]int) []string {
	var errs []string
	for _, b := range bu.Buckets {
		if counts[b.Name] > b.Max {
			errs = append(errs, fmt.Sprintf("%s: %d lines > budget %d (+%d)", b.Name, counts[b.Name], b.Max, counts[b.Name]-b.Max))
		}
	}
	return errs
}

// Ratchet lowers maxima to current counts. It never raises one.
func (bu *Budget) Ratchet(counts map[string]int) {
	for i := range bu.Buckets {
		if c, ok := counts[bu.Buckets[i].Name]; ok && c < bu.Buckets[i].Max {
			bu.Buckets[i].Max = c
		}
	}
}

// Baseline sets every maximum to the count rounded up to the next multiple of
// round. It is the one operation here that may raise a ceiling, which is why it
// is a separate flag and an owner's commit.
//
// It exists because a ceiling at exactly the count is a ceiling a one-line pull
// request fails, which is what the release review found: every bucket was on its
// own number, so adding a comment was over budget. The margin has to be small
// enough to be a ceiling and round enough that nobody negotiates it, and the
// next hundred is both — under one percent of every bucket here, and not a
// number anybody can argue was chosen to fit their change.
func (bu *Budget) Baseline(counts map[string]int, round int) {
	if round < 1 {
		round = 1
	}
	for i := range bu.Buckets {
		c, ok := counts[bu.Buckets[i].Name]
		if !ok {
			continue
		}
		bu.Buckets[i].Max = ((c + round - 1) / round) * round
	}
}

// reserved adds the acceptance round to every count, which is how --allow reaches
// Baseline without changing what a ceiling means: the rule for a ceiling is still
// "the count, rounded up to the next multiple of --round", and --allow says which
// count — the one the acceptance round arrives on rather than the one the feature
// leaves behind.
//
// The tool states that cost rather than letting each branch negotiate one. An
// acceptance round is this program's review, which lands its own test file on the
// branch after the feature's lines are already counted: T-0031 re-baselined
// `go_test` to the next hundred of a count ending in 79, which left 21 lines, and
// the round then brought a file of 139, so `--check` refused the review for the
// crime of reading the delivery. The reservation is measured from what this
// repository already holds — the largest single acceptance-round test file here is
// 499 lines, kit/httpx/review_surfaces_test.go — so a branch that expects to be
// reviewed passes --allow 500 for the bucket a review file is counted in, which is
// go_test, and leaves every other bucket unreserved, because a review round adds
// no production Go and no shipped module.
func reserved(counts map[string]int, allow int) map[string]int {
	if allow <= 0 {
		return counts
	}
	out := make(map[string]int, len(counts))
	for name, c := range counts {
		out[name] = c + allow
	}
	return out
}

// sourceFiles lists what the repository counts as its source: everything git
// tracks, plus everything it would track if committed now. A budget that
// ignored new files would only fail after the commit that broke it, which is
// the one place a ratchet is useless; --exclude-standard keeps build output and
// anything else .gitignore names out of the count.
func sourceFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z", "-c", "-o", "--exclude-standard")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	parts := bytes.Split(out, []byte{0})
	files := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) > 0 {
			files = append(files, string(p))
		}
	}
	return files, nil
}

func main() {
	os.Exit(run())
}

// run re-exposes exit semantics (2 = operational, 1 = over budget, 0 = ok) as
// a return of the testable flow. `go run` reports 1 for both failure codes, so
// the distinction only survives when the compiled binary is invoked directly.
func run() int {
	var (
		root   = flag.String("root", ".", "repository root containing loc-budget.json")
		check  = flag.Bool("check", false, "exit 1 when any bucket exceeds its max")
		write  = flag.Bool("write", false, "lower maxima to current counts")
		bucket = flag.String("bucket", "", "with --write: only this bucket")
		round  = flag.Int("round", 0, "with --write: re-baseline every max to the count rounded up to this multiple")
		allow  = flag.Int("allow", 0, "with --write --round: reserve this many lines of an acceptance round before rounding up (500 for go_test, 0 elsewhere)")
	)
	flag.Parse()

	budgetPath := filepath.Join(*root, "loc-budget.json")
	raw, err := os.ReadFile(budgetPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var budget Budget
	if err := json.Unmarshal(raw, &budget); err != nil {
		fmt.Fprintln(os.Stderr, "parse loc-budget.json:", err)
		return 2
	}
	if err := budget.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "loc-budget.json:", err)
		return 2
	}
	if *bucket != "" && !budget.Has(*bucket) {
		fmt.Fprintf(os.Stderr, "unknown bucket %q\n", *bucket)
		return 2
	}
	if *allow != 0 && (*round <= 0 || !*write) {
		// Only --write --round reads an allowance: a plain --write ratchets down,
		// and --check reports the committed ceilings as they stand. Refuse rather
		// than let a flag be typed in and silently do nothing.
		fmt.Fprintln(os.Stderr, "--allow needs --write --round: nothing else reads it")
		return 2
	}
	files, err := sourceFiles(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	counts := map[string]int{}
	maxima := map[string]int{}
	for _, b := range budget.Buckets {
		n, err := b.Count(*root, files)
		if err != nil {
			fmt.Fprintln(os.Stderr, b.Name+":", err)
			return 2
		}
		counts[b.Name] = n
		maxima[b.Name] = b.Max
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Printf("%-16s %9d / %9d\n", n, counts[n], maxima[n])
	}

	if *write {
		switch {
		case *round > 0 && *bucket != "":
			budget.Baseline(reserved(map[string]int{*bucket: counts[*bucket]}, *allow), *round)
		case *round > 0:
			budget.Baseline(reserved(counts, *allow), *round)
		case *bucket != "":
			budget.Ratchet(map[string]int{*bucket: counts[*bucket]})
		default:
			budget.Ratchet(counts)
		}
		out, err := json.MarshalIndent(budget, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "encode loc-budget.json:", err)
			return 2
		}
		if err := os.WriteFile(budgetPath, append(out, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Println("loc-budget.json updated")
	}
	if *check {
		if errs := budget.Check(counts); len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintln(os.Stderr, "OVER BUDGET:", e)
			}
			return 1
		}
		fmt.Println("loc budget OK")
	}
	return 0
}
