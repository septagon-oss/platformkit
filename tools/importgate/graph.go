package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

type edge struct {
	from, to, ownerPath, context, suite string
}

// moduleRoot is one repository Go resolved for this build: its module path and
// the directory that holds its source, which is where its own declarations live.
// The longest declared root owns a package path.
type moduleRoot struct{ path, dir string }

type graph struct {
	self     string
	dir      string
	roots    []moduleRoot
	standard map[string]bool
	// owned holds every module of the main module with a package in this build
	// context, sorted and deduplicated. A module with no cross-module edge is in
	// it, which is what lets a completeness rule be answered without edges.
	owned []string
	edges []edge
}

// root reports the declared repository that owns a package path.
func (g *graph) root(name string) string {
	root := ""
	for _, candidate := range g.roots {
		if (name == candidate.path || strings.HasPrefix(name, candidate.path+"/")) && len(candidate.path) > len(root) {
			root = candidate.path
		}
	}
	return root
}

func normalize(g *graph, packages []goPackage) error {
	indexed := map[string]goPackage{}
	for _, p := range packages {
		if p.Error != nil || len(p.DepsErrors) > 0 || p.Incomplete {
			return fmt.Errorf("go list %s: incomplete package; error: %+v; dependency errors: %+v", p.ImportPath, p.Error, p.DepsErrors)
		}
		if p.ImportPath == "" || p.Name == "" || p.Dir == "" || (!p.Standard && (p.Module == nil || p.Module.Path == "")) {
			return fmt.Errorf("go list %q: missing package identity", p.ImportPath)
		}
		indexed[p.ImportPath] = p
	}
	var edges []edge
	var owned []string
	add := func(name string) {
		if !slices.Contains(owned, name) {
			owned = append(owned, name)
		}
	}
	for _, p := range packages {
		if generatedMain(p, indexed) {
			continue
		}
		if p.Module == nil || p.Module.Path != g.self {
			continue
		}
		if !p.Module.Main {
			return fmt.Errorf("go list %s: inconsistent main module identity", p.ImportPath)
		}
		if p.ForTest != "" {
			if err := validateVariant(p, indexed); err != nil {
				return err
			}
			continue
		}
		// Only ordinary source names a module: a rebuilt test variant carries a
		// bracketed identity, and the package it was rebuilt for already stands.
		if module, _, _ := g.owner(p.ImportPath); module != "" {
			add(module)
		}
		if !cleanImport(p.ImportPath) || !(p.ImportPath == g.self || strings.HasPrefix(p.ImportPath, g.self+"/")) {
			return fmt.Errorf("go list %q: invalid source identity", p.ImportPath)
		}
		relative, err := filepath.Rel(g.dir, p.Dir)
		expected := strings.TrimPrefix(strings.TrimPrefix(p.ImportPath, g.self), "/")
		if expected == "" {
			expected = "."
		}
		if err != nil || filepath.ToSlash(relative) != expected {
			return fmt.Errorf("go list %s: package directory does not match module", p.ImportPath)
		}
		for _, kind := range []string{"production", "internal-test", "external-test"} {
			part, err := sourceEdges(p, kind, filepath.ToSlash(relative), indexed)
			if err != nil {
				return err
			}
			edges = append(edges, part...)
		}
	}
	slices.Sort(owned)
	g.owned, g.edges = owned, edges
	return nil
}

func generatedMain(p goPackage, records map[string]goPackage) bool {
	base, ok := records[strings.TrimSuffix(p.ImportPath, ".test")]
	if !ok || p.ImportPath != base.ImportPath+".test" || p.Name != "main" || p.Dir != base.Dir || len(base.TestGoFiles)+len(base.XTestGoFiles) == 0 {
		return false
	}
	if p.ForTest != "" && p.ForTest != base.ImportPath || len(p.GoFiles) == 0 {
		return false
	}
	for _, file := range p.GoFiles {
		if !filepath.IsAbs(file) || filepath.Dir(file) == p.Dir {
			return false
		}
	}
	return slices.Contains(p.Imports, "testing") && slices.Contains(p.Imports, "testing/internal/testdeps") && slices.Contains(p.Imports, "os")
}

func canonical(name string, records map[string]goPackage) (string, error) {
	base, annotation, bracketed := strings.Cut(name, " [")
	if !bracketed {
		if !cleanImport(name) {
			return "", fmt.Errorf("invalid import identity %q", name)
		}
		return name, nil
	}
	tested, ok := strings.CutSuffix(annotation, ".test]")
	p, exists := records[name]
	testPackage, hasTest := records[tested]
	if !ok || !cleanImport(base) || !cleanImport(tested) || !hasTest || len(testPackage.TestGoFiles)+len(testPackage.XTestGoFiles) == 0 {
		return "", fmt.Errorf("unresolved test identity %q", name)
	}
	if exists && p.ForTest != tested {
		return "", fmt.Errorf("inconsistent tested package for %q", name)
	}
	return base, nil
}

func imports(p goPackage, values []string, records map[string]goPackage) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if mapped, ok := p.ImportMap[value]; ok {
			value = mapped
		}
		name, err := canonical(value, records)
		if err != nil {
			return nil, fmt.Errorf("go list %s: %w", p.ImportPath, err)
		}
		result = append(result, name)
	}
	return result, nil
}

func validateVariant(p goPackage, records map[string]goPackage) error {
	name, err := canonical(p.ImportPath, records)
	if err != nil {
		return err
	}
	baseName := name
	if name == p.ForTest+"_test" {
		baseName = p.ForTest
	}
	base, ok := records[baseName]
	if !ok || base.ForTest != "" || base.Dir != p.Dir {
		return fmt.Errorf("go list %s: missing ordinary-source provenance", p.ImportPath)
	}
	if tested, ok := records[p.ForTest]; !ok || len(tested.TestGoFiles)+len(tested.XTestGoFiles) == 0 {
		return fmt.Errorf("go list %s: missing tested package provenance", p.ImportPath)
	}
	if name == p.ForTest && len(base.TestGoFiles) == 0 || name == p.ForTest+"_test" && len(base.XTestGoFiles) == 0 {
		return fmt.Errorf("go list %s: missing test file provenance", p.ImportPath)
	}
	if name != p.ForTest && name != p.ForTest+"_test" {
		actual, err := imports(p, p.Imports, records)
		if err != nil {
			return err
		}
		ordinary, err := imports(base, base.Imports, records)
		if err != nil {
			return err
		}
		for _, target := range actual {
			if !slices.Contains(ordinary, target) {
				return fmt.Errorf("go list %s: missing production provenance for %s", p.ImportPath, target)
			}
		}
	}
	return nil
}

func sourceEdges(p goPackage, kind, directory string, records map[string]goPackage) ([]edge, error) {
	from, suite, selected := p.ImportPath, "", p.Imports
	if kind != "production" {
		files, values, suffix, name := p.TestGoFiles, p.TestImports, "internal", p.Name
		if kind == "external-test" {
			files, values, suffix, name = p.XTestGoFiles, p.XTestImports, "external", p.Name+"_test"
			from += "_test"
		}
		if len(files) == 0 {
			if len(values) != 0 {
				return nil, fmt.Errorf("go list %s: test imports without test files", p.ImportPath)
			}
			return nil, nil
		}
		variant, ok := records[from+" ["+p.ImportPath+".test]"]
		if !ok || variant.ForTest != p.ImportPath || variant.Name != name || variant.Dir != p.Dir {
			return nil, fmt.Errorf("go list %s: missing %s provenance", p.ImportPath, kind)
		}
		for _, file := range files {
			if !strings.HasSuffix(file, "_test.go") || !slices.Contains(variant.GoFiles, file) {
				return nil, fmt.Errorf("go list %s: missing test file provenance for %q", p.ImportPath, file)
			}
		}
		compiled, err := imports(variant, variant.Imports, records)
		if err != nil {
			return nil, err
		}
		direct, err := imports(p, values, records)
		if err != nil {
			return nil, err
		}
		for _, target := range direct {
			if !slices.Contains(compiled, target) {
				return nil, fmt.Errorf("go list %s: missing test import provenance for %s", p.ImportPath, target)
			}
		}
		ordinary, err := imports(p, p.Imports, records)
		if err != nil {
			return nil, err
		}
		for _, target := range compiled {
			if !slices.Contains(direct, target) && (kind == "external-test" || !slices.Contains(ordinary, target)) {
				return nil, fmt.Errorf("go list %s: import %s has no source provenance", variant.ImportPath, target)
			}
		}
		suite, selected = directory+"@"+suffix, values
	}
	targets, err := imports(p, selected, records)
	if err != nil {
		return nil, err
	}
	result := make([]edge, 0, len(targets))
	for _, target := range targets {
		result = append(result, edge{from, target, p.ImportPath, kind, suite})
	}
	return result, nil
}
