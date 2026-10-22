package main

import (
	"fmt"
	"go/ast"
	"go/doc"
	"go/token"
	"maps"
	"slices"
	"strings"
)

func (m *measurement) stutters(fset *token.FileSet, path string, file *ast.File) {
	pkg := strings.ToLower(file.Name.Name)
	if pkg == "main" || len(pkg) <= 2 {
		return
	}
	check := func(name *ast.Ident) {
		if name.IsExported() && strings.HasPrefix(strings.ToLower(name.Name), pkg) {
			m.add("stutters", path, file.Name.Name+"."+name.Name, fset.PositionFor(name.Pos(), false).Line, 1)
		}
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Body != nil {
				check(d.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					check(s.Name)
				case *ast.ValueSpec:
					for _, name := range s.Names {
						check(name)
					}
				}
			}
		}
	}
}

func (m *measurement) httpSurface(fset *token.FileSet, files map[string]*ast.File) error {
	for _, path := range slices.Sorted(maps.Keys(files)) {
		file := files[path]
		if file.Name.Name != "httpx" {
			return fmt.Errorf("%q: expected package httpx, got %s", path, file.Name.Name)
		}
	}
	pkg := doc.New(&ast.Package{Name: "httpx", Files: files}, "kit/httpx", doc.PreserveAST)
	add := func(name string, pos token.Pos) {
		if name == "" || name[0] < 'A' || name[0] > 'Z' {
			return
		}
		p := fset.PositionFor(pos, false)
		m.add("httpx_exports", p.Filename, name, p.Line, 1)
	}
	values := func(groups []*doc.Value) {
		for _, value := range groups {
			if len(value.Names) == 0 {
				continue
			}
			for _, spec := range value.Decl.Specs {
				for _, name := range spec.(*ast.ValueSpec).Names {
					if name.Name == value.Names[0] {
						add(strings.Join(value.Names, ", "), name.Pos())
					}
				}
			}
		}
	}
	funcs := func(functions []*doc.Func) {
		for _, fn := range functions {
			add(fn.Name, fn.Decl.Name.Pos())
		}
	}
	values(pkg.Consts)
	values(pkg.Vars)
	funcs(pkg.Funcs)
	for _, typ := range pkg.Types {
		for _, spec := range typ.Decl.Specs {
			if t := spec.(*ast.TypeSpec); t.Name.Name == typ.Name {
				add(typ.Name, t.Name.Pos())
			}
		}
		values(typ.Consts)
		values(typ.Vars)
		funcs(typ.Funcs)
	}
	return nil
}
