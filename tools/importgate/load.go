package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

type command func(context.Context, string, ...string) ([]byte, error)

type module struct {
	Path, Dir string
	Main      bool
	Error     *struct{ Err string }
}

type packageError struct {
	ImportStack []string
	Pos, Err    string
}

type goPackage struct {
	ImportPath, Dir, Name, ForTest     string
	Module                             *module
	Standard, Incomplete               bool
	GoFiles, TestGoFiles, XTestGoFiles []string
	Imports, TestImports, XTestImports []string
	ImportMap                          map[string]string
	Error                              *packageError
	DepsErrors                         []packageError
}

func runGo(ctx context.Context, root string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: go %s: %w\n%s", root, strings.Join(args, " "), err, stderr.String())
	}
	return data, nil
}

func decode[T any](data []byte) ([]T, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var records []T
	for {
		var record T
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			return records, nil
		}
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
}

func cleanImport(value string) bool {
	if value == "" || path.Clean(value) != value || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "-") {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~/", c)) {
			return false
		}
	}
	for part := range strings.SplitSeq(value, "/") {
		if part == "." || part == ".." || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") {
			return false
		}
	}
	return true
}

func load(ctx context.Context, root string, dependencies []string, call command) (*graph, error) {
	for _, dependency := range dependencies {
		if !cleanImport(dependency) {
			return nil, fmt.Errorf("%s: invalid dependency module path %q", root, dependency)
		}
	}
	flags, err := call(ctx, root, "env", "GOFLAGS")
	if err != nil {
		return nil, err
	}
	for flag := range strings.FieldsSeq(string(flags)) {
		if strings.Trim(flag, "\"'") == "-mod=mod" {
			return nil, fmt.Errorf("%s: -mod=mod may update module files; use readonly or vendor resolution", root)
		}
	}
	data, err := call(ctx, root, "list", "-m", "-json")
	if err != nil {
		return nil, err
	}
	mods, err := decode[module](data)
	if err != nil || len(mods) != 1 || !mods[0].Main || !cleanImport(mods[0].Path) || mods[0].Error != nil {
		return nil, fmt.Errorf("%s: expected one valid main module: %v; metadata: %s", root, err, data)
	}
	main := mods[0]
	actual, err := filepath.EvalSymlinks(root)
	if err != nil || main.Dir != actual {
		return nil, fmt.Errorf("%s: main module directory is %q, expected repository root", root, main.Dir)
	}
	roots := []moduleRoot{{main.Path, main.Dir}}
	for _, dependency := range dependencies {
		if slices.ContainsFunc(roots, func(root moduleRoot) bool { return root.path == dependency }) {
			continue
		}
		data, err := call(ctx, root, "list", "-m", "-json", dependency)
		if err != nil {
			return nil, err
		}
		mods, err := decode[module](data)
		if err != nil || len(mods) != 1 || mods[0].Path != dependency || mods[0].Error != nil {
			return nil, fmt.Errorf("%s: unresolved dependency %q: %v; metadata: %s", root, dependency, err, data)
		}
		roots = append(roots, moduleRoot{dependency, mods[0].Dir})
	}
	return loadPackages(ctx, root, main, roots, call)
}

func loadPackages(ctx context.Context, root string, main module, roots []moduleRoot, call command) (*graph, error) {
	data, err := call(ctx, root, "list", "-f", "{{.ImportPath}}", "std")
	if err != nil {
		return nil, err
	}
	standard := map[string]bool{}
	for name := range strings.FieldsSeq(string(data)) {
		if !cleanImport(name) {
			return nil, fmt.Errorf("%s: invalid standard package %q", root, name)
		}
		standard[name] = true
	}
	if len(standard) == 0 {
		return nil, fmt.Errorf("%s: empty standard package metadata", root)
	}
	data, err = call(ctx, root, "list", "-json", "-test", "./...")
	if err != nil {
		return nil, err
	}
	packages, err := decode[goPackage](data)
	if err != nil {
		return nil, fmt.Errorf("%s: decode go list: %w", root, err)
	}
	g := &graph{self: main.Path, dir: main.Dir, roots: roots, standard: standard}
	if err := normalize(g, packages); err != nil {
		return nil, fmt.Errorf("%s: %w", root, err)
	}
	return g, nil
}
