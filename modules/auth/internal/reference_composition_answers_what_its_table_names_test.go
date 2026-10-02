package internal_test

// The surface table read against the composition the reference application runs.
//
// surface_test.go is the module's own record of its whole surface, and root put
// that record in charge of it (2026-10-01): no literal count, one table, read
// back out of httpx.API.Mounted. Its third case mounts the email-confirmation
// policy because apps/platformkit/modules.go chooses that policy, and it calls
// that composition the application's. It is not, quite: it wires no provider
// port, while the application passes OIDCProviders: tenantProviders{…}
// unconditionally. module.go:269 gates the two OIDC legs on
// `deps.OIDC.Issuer != "" || deps.OIDCProviders != nil`, deliberately, "not the
// installation's issuer alone, which would refuse to mount the two legs for a
// deployment whose providers are all per-tenant" — which is this brief's whole
// subject. So the arrangement a deployment actually ships (the port wired, the
// tenant table behind it still empty, because a tenant bootstrap just created has
// no issuer row) is mounted by none of the three cases.
//
// Read here, that arrangement answers 25 operations and every one of them is in
// the table, so the table does cover the application: the legs the application
// mounts are rows it names. What nothing guarded was that this stays true — the
// application's composition can gain a door (a third registration policy, a leg
// mounted on a new condition) that the three compositions in that file cannot
// stage, because none of them is the application's. So this case reads:
//
//   - three facts out of the composition's own syntax tree — auth.Deps names
//     EmailRegistration, and names OIDCProviders with something behind it other
//     than nil, and names neither open Registration nor ApprovalRegistration —
//     so the arrangement cannot drift from the application without failing here,
//     and the assertion never rests on a sentence a defect would have to print;
//   - the mount record of a module built with exactly that shape, compared with
//     the module's own table plus the two mailbox doors. The OIDC legs have to be
//     in that set, because the table's rows say an installation whose providers are
//     per tenant answers them.
//
// It passes while the module answers 25 operations there and every one of them is
// named in surface_test.go. It fails when a door appears in the application's
// shape that the table does not carry — a leg mounted by a condition nobody
// re-recorded, or a policy door the application stopped choosing — which is the
// case the existing three compositions cannot see because none of them is this one.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// appComposition is the reference application's own file: the literal list
// somebody wrote down, which is the only authority on what the app composes.
const appComposition = "../../../apps/platformkit/modules.go"

func TestTheReferenceApplicationCompositionAnswersWhatItsTableNames(t *testing.T) {
	fields, values := authDepsFields(t)
	for _, want := range []string{"EmailRegistration", "OIDCProviders"} {
		if !fields[want] {
			t.Fatalf("apps/platformkit/modules.go composes auth without %s, and this case mounts a "+
				"composition the reference application no longer runs: read the list again and re-record "+
				"the expected set in surface_test.go beside it", want)
		}
	}
	for _, absent := range []string{"Registration", "ApprovalRegistration"} {
		if fields[absent] {
			t.Fatalf("apps/platformkit/modules.go now names %s beside EmailRegistration: two registration "+
				"policies answer at once, and neither this case nor surface_test.go's table describes that", absent)
		}
	}
	if values["OIDCProviders"] == "nil" {
		t.Fatal("apps/platformkit/modules.go names OIDCProviders but hands the module nil: no tenant can " +
			"reach a provider and the two OIDC legs are not mounted, so this case is reading a composition " +
			"with a field on it rather than the one the application runs")
	}

	// The application's shape: an email-confirmation policy, no installation-wide
	// issuer, and the per-tenant provider port wired with nothing behind it yet —
	// which is what a tenant that bootstrap just created looks like.
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	_, _, _, api := mountRecorded(t, conn, auth.OIDC{}, false, emailSignup, func(deps *auth.Deps) {
		deps.OIDCProviders = mapProviders{}
	})

	want := make([]route, 0, len(wholeSurface)+len(mailboxDoors))
	want = append(want, wholeSurface...)
	want = append(want, mailboxDoors...)

	got := mountedHere(api)
	if len(got) != len(want) {
		t.Fatalf("the composition the reference application runs answered %d operations, its own table "+
			"names %d: %s", len(got), len(want), diff(got, want))
	}
	if d := diff(got, want); d != "" {
		t.Fatalf("the module's table does not name the surface the reference application answers: %s", d)
	}
}

// authDepsFields is the set of field names the application's auth.Deps literal
// sets, and the text of each value, read out of its syntax tree rather than
// counted out of its text.
func authDepsFields(t *testing.T) (map[string]bool, map[string]string) {
	t.Helper()
	path, err := filepath.Abs(appComposition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("read the reference composition: %v", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	fields := map[string]bool{}
	values := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if !authDepsType(lit.Type) {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			fields[key.Name] = true
			values[key.Name] = exprText(fset, kv.Value)
		}
		return true
	})
	if len(fields) == 0 {
		t.Fatal("no auth.Deps literal in apps/platformkit/modules.go: the composition moved, and this case " +
			"belongs beside wherever it went")
	}
	return fields, values
}

// exprText is the expression as it was written, so a field handed nil reads as
// nil here rather than as whatever the file's own name for it is.
func exprText(_ *token.FileSet, expr ast.Expr) string { return types.ExprString(expr) }

// authDepsType recognises auth.Deps and authmodule.Deps alike: the application
// gives the module package whatever alias reads best in that file.
func authDepsType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Deps" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && (id.Name == "auth" || id.Name == "authmodule")
}
