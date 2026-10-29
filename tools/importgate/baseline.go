package main

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// baselineRow admits exactly one direct production import out of one contracts
// package: the package's directory in this repository, the exact dependency it
// imports, and the reason the strict rule does not yet take it away.
type baselineRow struct{ dir, target, reason string }

// baselineKey identifies one row, and so one admitted edge.
type baselineKey struct{ dir, target string }

// readBaseline loads the target repository's committed contract baseline. A
// missing file means no edge is admitted, so a repository that never needed one
// keeps the strict production-contract rule in full. A malformed or unreadable
// file is an error: a half-read baseline would admit less than it declares.
func readBaseline(root string, g *graph) (map[baselineKey]*baselineRow, error) {
	file := filepath.Join(root, "scripts", "imports-contract-baseline.txt")
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return map[baselineKey]*baselineRow{}, nil
	}
	if err != nil {
		return nil, err
	}
	return parseBaseline(file, string(data), g)
}

func parseBaseline(file, text string, g *graph) (map[baselineKey]*baselineRow, error) {
	if !utf8.ValidString(text) {
		return nil, fmt.Errorf("%s: contract baseline rows must be UTF-8", file)
	}
	rows := map[baselineKey]*baselineRow{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		row, err := parseBaselineRow(line, g)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, i+1, err)
		}
		key := baselineKey{row.dir, row.target}
		if rows[key] != nil {
			return nil, fmt.Errorf("%s:%d: duplicate edge %s -> %s", file, i+1, row.dir, row.target)
		}
		rows[key] = row
	}
	return rows, nil
}

func parseBaselineRow(line string, g *graph) (*baselineRow, error) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return nil, fmt.Errorf("expected contract package, exact dependency and a nonempty reason")
	}
	dir, target := fields[0], fields[1]
	_, rest, _ := g.owner(g.self + "/" + dir)
	if !cleanImport(dir) || !strings.HasPrefix(dir, "modules/") || strings.HasSuffix(dir, ".go") || !contract(rest) || helper(rest) {
		return nil, fmt.Errorf("contract package %q must name a contracts package of this module, not a test helper", dir)
	}
	if !cleanImport(target) || target == g.self+"/"+dir {
		return nil, fmt.Errorf("invalid dependency %q", target)
	}
	if owner, _, _ := g.owner(g.self + "/" + target); owner != "" {
		return nil, fmt.Errorf("dependency %q must be named by its full import path", target)
	}
	reason := line
	for _, field := range fields[:2] {
		reason = strings.TrimSpace(strings.TrimPrefix(reason, field))
	}
	return &baselineRow{dir, target, reason}, nil
}

// baselineNotices announces every row once, sorted, with the state the current
// graph gives it and its complete reason, in the shape the integration notices
// already use.
func baselineNotices(rows map[baselineKey]*baselineRow, carried map[baselineKey]bool) string {
	var text strings.Builder
	for _, key := range slices.SortedFunc(maps.Keys(rows), func(a, b baselineKey) int {
		return cmp.Or(cmp.Compare(a.dir, b.dir), cmp.Compare(a.target, b.target))
	}) {
		state := "carried by production source in this build context"
		if !carried[key] {
			state = "carried by no production source in this build context"
		}
		fmt.Fprintf(&text, "imports: contract baseline %s -> %s; %s; %s\n", key.dir, key.target, state, rows[key].reason)
	}
	return text.String()
}
