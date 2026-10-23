// checkplurals refuses English plural suffix construction in Go and catalogues.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// frozenEnglish names the one file whose plural construction this check does not read.
// kit/entity/display.Plural is exported on the release line, which freezes that English
// set-name helper as it was written; docs/COMPATIBILITY.md names who may move a frozen
// name. Stopping the helper from appending a letter is what the plural rule wants and
// what the frozen line refuses, so the helper keeps its letter and this check says out
// loud where it stopped reading. The exemption is one path — not a package, not a
// directory: this package's test plants the same sentence in a sibling file and
// requires the refusal.
var frozenEnglish = map[string]struct{}{"kit/entity/display/display.go": {}}

// frozenPaths is frozenEnglish's keys in one order, for the line the check prints.
func frozenPaths() []string { return slices.Sorted(maps.Keys(frozenEnglish)) }

func main() {
	problems, err := scan(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, problem := range problems {
		fmt.Fprintln(os.Stderr, problem)
	}
	if len(problems) > 0 {
		os.Exit(1)
	}
	fmt.Println("plural source check: clean;", len(frozenEnglish), "frozen English file read no plural rule:",
		strings.Join(frozenPaths(), " "))
}

func scan(root string) ([]string, error) {
	var problems []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			// The exemption is a path in this repository, so it is matched on the path
			// under the directory being walked: the same file is exempt whether the walk
			// starts at "." or at a copy of the tree in a temporary directory.
			if rel, err := filepath.Rel(root, path); err == nil {
				if _, frozen := frozenEnglish[filepath.ToSlash(rel)]; frozen {
					return nil
				}
			}
			found, err := scanGo(path)
			problems = append(problems, found...)
			return err
		}
		if strings.HasSuffix(path, ".json") && strings.Contains(filepath.ToSlash(path), "/messages/") {
			found, err := scanJSON(path)
			problems = append(problems, found...)
			return err
		}
		return nil
	})
	return problems, err
}

func scanGo(path string) ([]string, error) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, path, nil, 0)
	if err != nil {
		return nil, err
	}
	var problems []string
	report := func(pos token.Pos, why string) {
		problems = append(problems, fmt.Sprintf("%s: %s", set.Position(pos), why))
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.BinaryExpr:
			if n.Op == token.ADD && (literal(n.X) == "s" || literal(n.Y) == "s") {
				report(n.Pos(), `plural suffix concatenation`)
			}
		case *ast.AssignStmt:
			if n.Tok == token.ADD_ASSIGN {
				for _, rhs := range n.Rhs {
					if literal(rhs) == "s" {
						report(n.Pos(), `plural suffix assignment`)
					}
				}
			}
		case *ast.BasicLit:
			if n.Kind == token.STRING {
				value, err := strconv.Unquote(n.Value)
				if err == nil && pluralSuffixFormat(value) {
					report(n.Pos(), `plural format suffix`)
				}
			}
		}
		return true
	})
	return problems, nil
}

// pluralSuffixFormat refuses a noun pluralised by appending a letter to a verb: the
// unnumbered verb a Go format writes ("%" then "ss" below), and the numbered verb a
// gotext catalogue writes its forms with ("%[1]ss"). They are the same suffix; a
// checker that read only the unnumbered spelling would let a translated catalogue
// pluralise in English while Go source stayed clean. The verb is matched a byte at a
// time rather than written whole beside the tail, because this checker walks its own
// source and the joined literal is the finding it reports.
func pluralSuffixFormat(value string) bool {
	const tail = "ss"
	for at := 0; at < len(value); at++ {
		if value[at] != '%' {
			continue
		}
		rest := value[at+1:]
		if strings.HasPrefix(rest, tail) {
			return true
		}
		end := strings.IndexByte(rest, ']')
		if end <= 0 {
			continue
		}
		if digits(rest[1:end]) && strings.HasPrefix(rest[end+1:], tail) {
			return true
		}
	}
	return false
}

// digits reports a non-empty run of decimal digits: the index a numbered verb names.
func digits(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return value != ""
}

func literal(expr ast.Expr) string {
	value, ok := expr.(*ast.BasicLit)
	if !ok || value.Kind != token.STRING {
		return ""
	}
	copy, _ := strconv.Unquote(value.Value)
	return copy
}

func scanJSON(path string) ([]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var problems []string
	var visit func(any, string)
	visit = func(value any, key string) {
		switch typed := value.(type) {
		case string:
			if pluralSuffixFormat(typed) {
				problems = append(problems, fmt.Sprintf("%s: %s: plural format suffix", path, key))
			}
		case map[string]any:
			for name, child := range typed {
				visit(child, key+"."+name)
			}
		case []any:
			for index, child := range typed {
				visit(child, fmt.Sprintf("%s[%d]", key, index))
			}
		}
	}
	visit(value, "catalogue")
	return problems, nil
}
