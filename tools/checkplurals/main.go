// checkplurals refuses English plural suffix construction in Go and catalogues.
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
	"strconv"
	"strings"
)

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
	fmt.Println("plural source check: clean")
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

// pluralSuffixFormat refuses a noun pluralised by appending a letter to a verb:
// "%ss", and the numbered verb a gotext catalogue writes its forms with, "%[1]ss".
// They are the same suffix; a checker that read only the unnumbered spelling would
// let a translated catalogue pluralise in English while Go source stayed clean.
func pluralSuffixFormat(value string) bool {
	if strings.Contains(value, "%"+"ss") {
		return true
	}
	for at := 0; at+1 < len(value); {
		if value[at] != '[' || !strings.HasSuffix(value[:at], "%") {
			at++
			continue
		}
		end := strings.IndexByte(value[at:], ']')
		if end < 0 {
			return false
		}
		number := value[at+1 : at+end]
		if number != "" && strings.Trim(number, "0123456789") == "" && strings.HasPrefix(value[at+end+1:], "s"+"s") {
			return true
		}
		at += end + 1
	}
	return false
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
