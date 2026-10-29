package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

type grant struct {
	key, reason string
	targets     map[string]bool
}

func readGrants(root string, g *graph) (map[string]*grant, error) {
	file := filepath.Join(root, "scripts", "imports-integration.txt")
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return map[string]*grant{}, nil
	}
	if err != nil {
		return nil, err
	}
	return parseGrants(file, string(data), g)
}

func parseGrants(file, text string, g *graph) (map[string]*grant, error) {
	if !utf8.ValidString(text) {
		return nil, fmt.Errorf("%s: integration grants must be UTF-8", file)
	}
	rows := map[string]*grant{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		row, err := parseGrant(line, g)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, i+1, err)
		}
		if rows[row.key] != nil {
			return nil, fmt.Errorf("%s:%d: duplicate suite %q", file, i+1, row.key)
		}
		rows[row.key] = row
	}
	return rows, nil
}

func parseGrant(line string, g *graph) (*grant, error) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return nil, fmt.Errorf("expected suite, exact targets and a nonempty reason")
	}
	directory, kind, _ := strings.Cut(fields[0], "@")
	if !cleanImport(directory) || !strings.HasPrefix(directory, "modules/") || strings.HasSuffix(directory, ".go") || kind != "internal" && kind != "external" {
		return nil, fmt.Errorf("invalid suite key %q", fields[0])
	}
	owner, _, _ := g.owner(g.self + "/" + directory)
	if owner == "" || directory == "modules/" {
		return nil, fmt.Errorf("suite %q must name a module package", fields[0])
	}
	reason := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, fields[0])), fields[1]))
	row := &grant{fields[0], reason, map[string]bool{}}
	for target := range strings.SplitSeq(fields[1], ",") {
		root, rest, unknown := g.owner(target)
		if !cleanImport(target) || root == "" || rest != "" || unknown || target == owner {
			return nil, fmt.Errorf("invalid foreign constructor root %q", target)
		}
		if _, ok := row.targets[target]; ok {
			return nil, fmt.Errorf("duplicate constructor %q", target)
		}
		row.targets[target] = true
	}
	return row, nil
}

func notices(grants map[string]*grant, consumed map[grantTarget]bool) string {
	var text strings.Builder
	for _, key := range slices.Sorted(maps.Keys(grants)) {
		row := grants[key]
		var used, unused []string
		for _, target := range slices.Sorted(maps.Keys(row.targets)) {
			if consumed[grantTarget{key, target}] {
				used = append(used, target)
			} else {
				unused = append(unused, target)
			}
		}
		fmt.Fprintf(&text, "imports: integration %s; used [%s]; unused in this build context [%s]; %s\n", key, strings.Join(used, ","), strings.Join(unused, ","), row.reason)
	}
	return text.String()
}
