// Command pkgstate reports the package-level variables of a tree that are
// still writable once the package has been initialised: a value a caller can
// reach through, or a value somebody writes to after its declaration.
//
// A composition hands a module what it declares it needs; a writable
// package-level variable is a second way into the same state from anywhere in
// the process. The kernel is where such a state settles most easily, so the
// report reads kit/ by default.
//
// The output is a trend, not a ceiling: the run prints a count and exits 0.
//
// Usage: go run ./tools/pkgstate [-root DIR] [-under kit]
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pkgstate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "checkout to read")
	under := fs.String("under", "kit", "tree to report, relative to -root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	tree, err := load(*root, *under)
	if err != nil {
		fmt.Fprintf(stderr, "pkgstate: %v\n", err)
		return 2
	}
	findings := classify(tree)
	for _, f := range findings {
		fmt.Fprintf(stdout, "%s\t%s is %s\n", f.Where, f.Name, f.Why)
	}
	fmt.Fprintf(stdout, "pkgstate: %d of %d package-level variables under %s are still writable\n",
		len(findings), len(tree.decls), filepath.Join(*root, *under))
	return 0
}

// packageState is one parsed package: where it lives, the import path a caller
// writes it by, the variables it declares at package level, the types it
// declares with an Error method, and the type names it declares.
type packageState struct {
	dir        string
	importPath string
	variables  map[string]*declaration
	imports    map[string]string
	errorTypes map[string]bool
	typeDecls  map[string]ast.Expr
}

// declaration is one package-level var, the documents that speak for it, and
// the writes the second pass found against its name.
type declaration struct {
	where  string
	name   string
	spec   *ast.ValueSpec
	docs   []*ast.CommentGroup
	own    *packageState
	which  int // index of this name within spec.Names
	writes []string
}

// finding is one variable still writable, named where it is declared.
type finding struct {
	Where string
	Name  string
	Why   string
}

// sourceFile is one parsed file, the package it belongs to and the names it
// uses for what it imports.
type sourceFile struct {
	file    *ast.File
	rel     string
	pkg     *packageState
	imports map[string]string
}

// tree is what a run read: every package under the reported directory, the
// files that make them up, and every package-level variable among them.
type tree struct {
	fset   *token.FileSet
	dirs   map[string]*packageState
	byPath map[string]*packageState
	files  []*sourceFile
	decls  []*declaration
}

// load parses every production Go file under root/under and finds the writes
// among them. A file that does not parse, a tree that cannot be walked or a
// checkout that declares no module is an error: a report over half a tree would
// read as a whole one, and a run that exits 2 says it read nothing.
//
// The read is syntax alone. A variable whose type arrives through a call is
// counted only by what it was written with, and a write from outside the
// reported tree is not seen: both limits leave a writable variable unreported
// rather than naming one that is not writable.
func load(root, under string) (*tree, error) {
	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	t := &tree{fset: token.NewFileSet(), dirs: map[string]*packageState{}, byPath: map[string]*packageState{}}
	err = filepath.Walk(filepath.Join(root, under), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		return t.read(root, module, path)
	})
	if err != nil {
		return nil, err
	}
	for _, pkg := range t.dirs {
		t.byPath[pkg.importPath] = pkg
	}
	for _, f := range t.files {
		findWrites(t, f)
	}
	return t, nil
}

// read parses one file into the tree, adding it to the package of its directory.
func (t *tree) read(root, module, path string) error {
	file, err := parser.ParseFile(t.fset, path, nil, parser.ParseComments)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(rel)
	pkg := t.dirs[dir]
	if pkg == nil {
		pkg = &packageState{dir: dir, importPath: module + "/" + filepath.ToSlash(dir),
			variables: map[string]*declaration{}, imports: map[string]string{},
			errorTypes: map[string]bool{}, typeDecls: map[string]ast.Expr{}}
		t.dirs[dir] = pkg
	}
	src := &sourceFile{file: file, rel: rel, pkg: pkg, imports: map[string]string{}}
	for _, spec := range file.Imports {
		path := strings.Trim(spec.Path.Value, `"`)
		name := path[strings.LastIndex(path, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == "_" || name == "." {
			continue
		}
		// The name one file knows an imported package by is what lets
		// `registry.Items = …` be traced back to the package that declares it.
		src.imports[name] = path
		src.pkg.imports[name] = path
	}
	t.files = append(t.files, src)
	recordTypes(file, pkg)
	recordVariables(t, file, rel, pkg)
	return nil
}

// modulePath reads the module the checkout declares, which is what turns an
// import path back into a directory of this same tree.
func modulePath(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(line, "module"); ok {
			if name := strings.TrimSpace(rest); name != "" {
				return name, nil
			}
		}
	}
	return "", fmt.Errorf("%s declares no module", filepath.Join(root, "go.mod"))
}
