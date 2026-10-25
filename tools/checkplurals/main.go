// Command checkplurals refuses English plural construction: a letter appended to a word, or
// a format that pluralises by one, in Go source and in the catalogues under a messages/
// directory. A page asks the reader's own plural rule how a plural reads; it never spells
// one. kit/entity/display/display.go is read by no rule here: its exported Plural helper
// sits on the frozen release line, which fixes the letter it appends, and
// docs/COMPATIBILITY.md names who may move a frozen name. The exemption is one path, not a
// directory, so a sibling of the frozen helper stays refused.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// pluralVerb is the suffix that pluralises a verb: "%ss" as a Go format writes it and
// "%[1]ss" as a gotext catalogue numbers its forms. One suffix, refused in both spellings.
var pluralVerb = regexp.MustCompile(`%(?:\[\d+\])?ss`)

// frozenEnglish names the exempt paths, matched against the path under the root being walked,
// so a copy of the tree in a temporary directory is exempt exactly as this one is.
var frozenEnglish = []string{"kit/entity/display/display.go"}

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
	fmt.Println("plural source check: clean;", len(frozenEnglish), "frozen English file read no plural rule:", strings.Join(frozenEnglish, " "))
}

// scan returns every plural construction under root. Go source is read as the compiler reads
// it, so the rule refuses code and never a comment about the rule, and a Go test file is left
// unread, being where a planted refusal lives. A JSON file is read only under messages/.
func scan(root string) ([]string, error) {
	var problems []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		found, err := scanFile(root, path)
		problems = append(problems, found...)
		return err
	})
	return problems, err
}

// scanFile reads one file, or nothing when the plural rule has no view of it.
func scanFile(root, path string) ([]string, error) {
	slash := filepath.ToSlash(path)
	switch {
	case strings.HasSuffix(slash, ".go") && !strings.HasSuffix(slash, "_test.go"):
		rel, err := filepath.Rel(root, path)
		if err == nil && slices.Contains(frozenEnglish, filepath.ToSlash(rel)) {
			return nil, nil
		}
		return scanGo(path)
	case strings.HasSuffix(slash, ".json") && strings.Contains(slash, "/messages/"):
		return scanJSON(path)
	}
	return nil, nil
}

// scanGo reports a suffix a concatenation appends, a suffix a compound assignment appends,
// and a format pluralised by one, each at the node that writes it.
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
				report(n.Pos(), "plural suffix concatenation")
			}
		case *ast.AssignStmt:
			if n.Tok == token.ADD_ASSIGN {
				for _, rhs := range n.Rhs {
					if literal(rhs) == "s" {
						report(n.Pos(), "plural suffix assignment")
					}
				}
			}
		case *ast.BasicLit:
			if pluralVerb.MatchString(literal(n)) {
				report(n.Pos(), "plural format suffix")
			}
		}
		return true
	})
	return problems, nil
}

// literal answers the text of a string literal expression, and "" for anything else.
func literal(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	value, _ := strconv.Unquote(lit.Value)
	return value
}

// scanJSON reports a plural format in any string of a catalogue, wherever it sits in it.
func scanJSON(path string) ([]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var catalogue any
	if err := json.Unmarshal(body, &catalogue); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var problems []string
	var visit func(any, string)
	visit = func(node any, key string) {
		switch value := node.(type) {
		case string:
			if pluralVerb.MatchString(value) {
				problems = append(problems, fmt.Sprintf("%s: %s: plural format suffix", path, key))
			}
		case map[string]any:
			for name, child := range value {
				visit(child, key+"."+name)
			}
		case []any:
			for index, child := range value {
				visit(child, fmt.Sprintf("%s[%d]", key, index))
			}
		}
	}
	visit(catalogue, "catalogue")
	return problems, nil
}
