package internal

// Review round 5's pin, on the one number of the brief that no test holds.
//
// T-0100 exists partly because "RegisterRoutes is 253 lines … a list that long
// is a page a reader scrolls rather than a declaration they read", and its
// done-criterion 3 names the bound: no function exceeds 150 lines except a
// `cases` table. Nothing in this repository measures a function. The loc budget
// counts a bucket of files, not a span, so a route body written back into the
// list — the exact regression this split exists to prevent — moves no ceiling
// and fails no gate. It just makes the file read like a page again.
//
// So this case reads handler.go the way a reader does. Two assertions, both
// about the list rather than the answers it names:
//
//   - RegisterRoutes spans at most 150 lines, the brief's own bound;
//   - every registration declares its guard and hands over a factory call or a
//     named function, so no operation body is spelled out inside the list. The
//     kernel already panics at boot on the zero Auth (httpx.prepare), which is
//     why the second half is a shape and not only an argument count: a closure
//     passed inline is a guarded, declared route that is still forty lines of
//     prose in the middle of a table of contents.
//
// Both reach their assertion through what the declaration is — a span, a callee
// — never through a sentence a defect would have to print.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"testing"
)

// registerRoutesMaxLines is the brief's own ceiling ("no function exceeds 150
// lines except a cases table"), applied to the one function it names.
const registerRoutesMaxLines = 150

func TestTheRouteListIsADeclarationAndEveryRouteNamesItsGuard(t *testing.T) {
	src, err := os.ReadFile("handler.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "handler.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	var routes *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "RegisterRoutes" {
			routes = fn
		}
	}
	if routes == nil {
		t.Fatal("handler.go declares no RegisterRoutes: the list this case reads has moved to another " +
			"file, and this one belongs beside it")
	}
	if span := fset.Position(routes.End()).Line - fset.Position(routes.Pos()).Line + 1; span > registerRoutesMaxLines {
		t.Fatalf("RegisterRoutes spans %d lines (handler.go:%d-%d), over the %d the brief allows: the "+
			"route list is a page a reader scrolls again, which is what it was split for",
			span, fset.Position(routes.Pos()).Line, fset.Position(routes.End()).Line, registerRoutesMaxLines)
	}

	registrations := 0
	ast.Inspect(routes, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isRegister(call) {
			return true
		}
		registrations++
		where := "the " + ordinal(registrations) + " registration in the list"
		if len(call.Args) != 4 {
			t.Errorf("%s: httpx.Register takes (surface, operation, guard, handler) and got %d "+
				"arguments, so this route no longer declares how it is guarded", where, len(call.Args))
			return true
		}
		if _, guarded := call.Args[2].(*ast.CallExpr); !guarded {
			t.Errorf("%s: the guard is %s, not a guard call like httpx.Public(), httpx.SignedIn() or "+
				"httpx.Permission(...)", where, shape(call.Args[2]))
		}
		switch call.Args[3].(type) {
		case *ast.FuncLit:
			t.Errorf("%s: the handler is written out inside the list, so the list is a page: the answer "+
				"belongs in handlers.go beside the others", where)
		case *ast.CallExpr, *ast.Ident:
			// handleLogin(svc, cookies) or a named function — the shape every
			// route took when the bodies moved out, and the shape kept here.
		default:
			t.Errorf("%s: the handler is %s, neither a factory call nor a named function",
				where, shape(call.Args[3]))
		}
		return true
	})
	if registrations != 8 {
		t.Fatalf("handler.go registers %d operations, want the 8 the module answers with: this list is "+
			"the module's whole HTTP surface, so a route appearing or vanishing here is not a move",
			registrations)
	}
}

func isRegister(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "httpx" && sel.Sel.Name == "Register"
}

func ordinal(n int) string {
	switch n {
	case 1:
		return "first"
	case 2:
		return "second"
	case 3:
		return "third"
	case 4:
		return "fourth"
	case 5:
		return "fifth"
	case 6:
		return "sixth"
	case 7:
		return "seventh"
	case 8:
		return "eighth"
	}
	return strconv.Itoa(n) + "th"
}

func shape(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.CallExpr:
		return "a call to " + shape(x.Fun)
	case *ast.Ident:
		return "the identifier " + x.Name
	case *ast.SelectorExpr:
		return shape(x.X) + "." + x.Sel.Name
	case *ast.FuncLit:
		return "a function literal"
	case *ast.CompositeLit:
		return "a composite literal — the zero httpx.Auth, which the kernel panics on at boot"
	}
	return "an expression of another kind"
}
