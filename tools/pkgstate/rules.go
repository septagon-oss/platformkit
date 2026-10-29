package main

// The rules the report reads, in the shape the tree answers them: a package-level
// var is a candidate unless it is an error sentinel or marked immutable, and a
// candidate is reported when its value can be reached through or when somebody
// writes to it after its declaration.

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strings"
)

// recordTypes notes the types a package declares, and which of them have an
// Error() string method: a variable of one of those holds a sentinel, not state.
func recordTypes(file *ast.File, pkg *packageState) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					pkg.typeDecls[ts.Name.Name] = ts.Type
				}
			}
		case *ast.FuncDecl:
			if d.Recv == nil || d.Name.Name != "Error" ||
				d.Type.Params.NumFields() != 0 || d.Type.Results.NumFields() != 1 {
				continue
			}
			result, ok := d.Type.Results.List[0].Type.(*ast.Ident)
			if !ok || result.Name != "string" {
				continue
			}
			if name := typeName(d.Recv.List[0].Type); name != "" {
				pkg.errorTypes[name] = true
			}
		}
	}
}

// typeName names the type a receiver or a variable is declared as, or answers
// the empty string when it is written any other way.
func typeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeName(t.X)
	}
	return ""
}

// recordVariables takes the var declarations at the top level of a file: those
// are the names a caller reaches without being handed anything.
func recordVariables(t *tree, file *ast.File, rel string, pkg *packageState) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			docs := []*ast.CommentGroup{vs.Doc, vs.Comment, gen.Doc}
			for i, name := range vs.Names {
				if name.Name == "_" {
					continue
				}
				d := &declaration{
					where: fmt.Sprintf("%s:%d", rel, t.fset.Position(name.Pos()).Line),
					name:  name.Name, spec: vs, docs: docs, own: pkg, which: i,
				}
				pkg.variables[name.Name] = d
				t.decls = append(t.decls, d)
			}
		}
	}
}

// findWrites records every assignment in one file that lands on a package-level
// variable of this tree, in its own package or through another package's name.
func findWrites(t *tree, src *sourceFile) {
	// One frame per node the walk descends into, so that the frame an enclosing
	// function opened is still open while its body is read.
	var frames []map[string]bool
	line := func(pos token.Pos) string {
		return fmt.Sprintf("%s:%d", src.rel, t.fset.Position(pos).Line)
	}
	ast.Inspect(src.file, func(n ast.Node) bool {
		if n == nil {
			if len(frames) > 0 {
				frames = frames[:len(frames)-1]
			}
			return false
		}
		var scope map[string]bool
		switch x := n.(type) {
		case *ast.FuncDecl:
			scope = localNames(x)
		case *ast.FuncLit:
			scope = localNames(x)
		case *ast.AssignStmt:
			if x.Tok != token.DEFINE {
				for _, lhs := range x.Lhs {
					assign(t, src, lhs, frames, line(x.Pos()))
				}
			}
		case *ast.IncDecStmt:
			assign(t, src, x.X, frames, line(x.Pos()))
		}
		frames = append(frames, scope)
		return true
	})
}

// assign records a write against the package-level variable a target names, if
// it names one. A target that a local of the same function shadows names no
// variable of the package, so it is left alone.
func assign(t *tree, src *sourceFile, lhs ast.Expr, frames []map[string]bool, where string) {
	switch target := lhs.(type) {
	case *ast.Ident:
		if shadowed(frames, target.Name) {
			return
		}
		if d := src.pkg.variables[target.Name]; d != nil {
			d.writes = append(d.writes, where)
		}
	case *ast.SelectorExpr:
		pkgName, ok := target.X.(*ast.Ident)
		if !ok || shadowed(frames, pkgName.Name) {
			return
		}
		path, ok := src.imports[pkgName.Name]
		if !ok {
			return
		}
		crossed := t.byPath[path]
		if crossed == nil {
			return
		}
		if d, ok := crossed.variables[target.Sel.Name]; ok {
			d.writes = append(d.writes, where)
		}
	}
}

// shadowed reports whether some enclosing function declares this name itself,
// which is what makes a write to it a write to the local. The answer is over
// every name a function declares anywhere, so a package variable written inside
// a function that happens to use the same name for a local is missed; a report
// that named one of those as a finding would not be believed.
func shadowed(frames []map[string]bool, name string) bool {
	for _, scope := range frames {
		if scope[name] {
			return true
		}
	}
	return false
}

// localNames collects the names one function declares: its receiver, its
// parameters and results, the type names it declares, and every name its body
// binds, including inside a closure it builds.
func localNames(fn ast.Node) map[string]bool {
	names := map[string]bool{}
	collect := func(exprs ...ast.Expr) {
		for _, expr := range exprs {
			if id, ok := expr.(*ast.Ident); ok {
				names[id.Name] = true
			}
		}
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Field:
			for _, id := range x.Names {
				names[id.Name] = true
			}
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				collect(x.Lhs...)
			}
		case *ast.RangeStmt:
			if x.Tok == token.DEFINE {
				collect(x.Key, x.Value)
			}
		case *ast.DeclStmt:
			if gen, ok := x.Decl.(*ast.GenDecl); ok {
				for _, spec := range gen.Specs {
					switch s := spec.(type) {
					case *ast.ValueSpec:
						for _, id := range s.Names {
							names[id.Name] = true
						}
					case *ast.TypeSpec:
						collect(s.Name)
					}
				}
			}
		}
		return true
	})
	return names
}

// classify answers, in tree order, why each variable is still writable.
func classify(t *tree) []finding {
	var out []finding
	for _, d := range t.decls {
		if why := whyWritable(d); why != "" {
			out = append(out, finding{Where: d.where, Name: d.name, Why: why})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Where < out[j].Where })
	return out
}

// whyWritable names the rule that reports a variable, or answers the empty
// string when no rule does.
func whyWritable(d *declaration) string {
	if sentinel(d) || markedImmutable(d) {
		return ""
	}
	kind := reachableThrough(d)
	switch {
	case kind != "" && len(d.writes) > 0:
		return fmt.Sprintf("reachable through its %s and written at %s", kind, d.writes[0])
	case kind != "":
		return "reachable through its " + kind
	case len(d.writes) > 0:
		return fmt.Sprintf("written outside its declaration at %s", d.writes[0])
	}
	return ""
}

// sentinel reports whether the variable holds an error: one built by errors.New
// or fmt.Errorf, or of a type this package declares with an Error method. Those
// are values compared with ==, not state to be updated.
func sentinel(d *declaration) bool {
	if name := typeName(d.spec.Type); name != "" && d.own.errorTypes[name] {
		return true
	}
	call, ok := d.value().(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := fn.X.(*ast.Ident)
	if !ok {
		return false
	}
	return (pkg.Name == "errors" && fn.Sel.Name == "New") || (pkg.Name == "fmt" && fn.Sel.Name == "Errorf")
}

// markedImmutable reports whether the variable's own document carries the line
// that says it is written once: `Immutable: <why it is safe>`. A table the
// composition reads and never edits is a fact, and the reason belongs on it.
func markedImmutable(d *declaration) bool {
	for _, group := range d.docs {
		if group == nil {
			continue
		}
		for _, comment := range group.List {
			for line := range strings.SplitSeq(comment.Text, "\n") {
				text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "//"))
				if strings.HasPrefix(strings.TrimSpace(text), "Immutable:") {
					return true
				}
			}
		}
	}
	return false
}

// reachableThrough names the kind of value a variable hands out — the kinds
// whose contents change with no assignment to find — or the empty string when
// writing the variable itself is the only way to change it.
func reachableThrough(d *declaration) string {
	if kind := kindOf(d.spec.Type, d.own); kind != "" {
		return kind
	}
	switch v := d.value().(type) {
	case *ast.CompositeLit:
		return kindOf(v.Type, d.own)
	case *ast.UnaryExpr:
		if v.Op == token.AND {
			return "pointer"
		}
	}
	return ""
}

// kindOf names the mutable shape of a type expression, following one hop into a
// type declared in the same package. A call's result has no type here, because
// the report builds no type information.
func kindOf(expr ast.Expr, pkg *packageState) string {
	switch t := expr.(type) {
	case *ast.ArrayType:
		if t.Len == nil {
			return "slice"
		}
		return "array"
	case *ast.MapType:
		return "map"
	case *ast.StarExpr:
		return "pointer"
	case *ast.ChanType:
		return "channel"
	case *ast.Ident:
		if next, ok := pkg.typeDecls[t.Name]; ok {
			return kindOf(next, pkg)
		}
	}
	return ""
}

// value answers the initialiser written for this one name, when the declaration
// gives every name its own.
func (d *declaration) value() ast.Expr {
	if len(d.spec.Values) != len(d.spec.Names) {
		return nil
	}
	return d.spec.Values[d.which]
}
