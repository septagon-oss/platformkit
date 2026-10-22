package main

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"slices"
)

type offender struct {
	measure, path, name string
	line, size          int
}

type measurement struct {
	counts    budget
	offenders []offender
}

func (m *measurement) add(key, path, name string, line, size int) {
	m.counts[key]++
	m.offenders = append(m.offenders, offender{key, path, name, line, size})
}

func measure(files []source) (measurement, error) {
	m := measurement{counts: zeroBudget()}
	fset := token.NewFileSet()
	httpFiles := map[string]*ast.File{}
	for _, src := range files {
		file, err := parser.ParseFile(fset, src.path, src.data, parser.ParseComments)
		if err != nil {
			return measurement{}, fmt.Errorf("parse %q: %w", src.path, err)
		}
		if lines := bytes.Count(src.data, []byte{'\n'}); lines > 800 {
			m.add("files_over_800", src.path, src.path, 1, lines)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			line := fset.PositionFor(fn.Pos(), false).Line
			span := fset.PositionFor(fn.End(), false).Line - line + 1
			if span > 100 {
				m.add("funcs_over_100", src.path, functionName(fn), line, span)
			}
			if cc := complexity(fn.Body); cc > 30 {
				m.add("cc_over_30", src.path, functionName(fn), line, cc)
			}
		}
		m.comments(fset, src.path, file.Comments)
		m.stutters(fset, src.path, file)
		if path.Dir(src.path) == "kit/httpx" {
			httpFiles[src.path] = file
		}
	}
	if len(httpFiles) == 0 {
		return measurement{}, fmt.Errorf("kit/httpx: required Go package is absent")
	}
	if err := m.httpSurface(fset, httpFiles); err != nil {
		return measurement{}, err
	}
	slices.SortFunc(m.offenders, func(a, b offender) int {
		return cmp.Or(cmp.Compare(a.measure, b.measure), cmp.Compare(a.path, b.path), cmp.Compare(a.line, b.line), cmp.Compare(a.name, b.name), cmp.Compare(a.size, b.size))
	})
	return m, nil
}

func complexity(body *ast.BlockStmt) int {
	cc := 1
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			cc++
		case *ast.CaseClause:
			if n.List != nil {
				cc++
			}
		case *ast.CommClause:
			if n.Comm != nil {
				cc++
			}
		case *ast.BinaryExpr:
			if n.Op == token.LAND || n.Op == token.LOR {
				cc++
			}
		}
		return true
	})
	return cc
}

func functionName(fn *ast.FuncDecl) string {
	if fn.Recv == nil {
		return fn.Name.Name
	}
	return receiverName(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

func receiverName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return receiverName(e.X)
	case *ast.IndexExpr:
		return receiverName(e.X)
	case *ast.IndexListExpr:
		return receiverName(e.X)
	case *ast.ParenExpr:
		return receiverName(e.X)
	}
	return "receiver"
}
