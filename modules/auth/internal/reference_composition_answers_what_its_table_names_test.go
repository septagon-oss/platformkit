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
	"os"
	"path/filepath"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// appComposition is the reference application's own file: the list somebody
// wrote down, which is the only authority on what the app composes. It used to
// be a composition file that built every Deps literal; the modules wire
// themselves now, so what this file reads out of the syntax tree is which
// registration door the app names in Use, and whether the module that answers
// the per-tenant provider port is composed at all.
const appComposition = "../../../apps/platformkit/app.go"

func TestTheReferenceApplicationCompositionAnswersWhatItsTableNames(t *testing.T) {
	names := authDepsFields(t)
	for _, want := range []string{"EmailRegistration"} {
		if !names[want] {
			t.Fatalf("apps/platformkit/app.go composes auth without %s in its Use list, and this case mounts "+
				"a composition the reference application no longer runs: read the list again and re-record "+
				"the expected set in surface_test.go beside it", want)
		}
	}
	for _, absent := range []string{"Registration", "ApprovalRegistration"} {
		if names[absent] {
			t.Fatalf("apps/platformkit/app.go now names %s beside EmailRegistration: two registration "+
				"policies answer at once, and neither this case nor surface_test.go's table describes that", absent)
		}
	}
	// The per-tenant provider port used to be one line of the app's Deps
	// literal, and the case below refuses it when the literal hands nil. The
	// app names no Deps literal any more: auth asks for the contract
	// authcontracts.OIDCProviders and tenant.Module is the composed answer to
	// it. An app that composed no tenant would mount neither OIDC leg, which is
	// the same emptiness `OIDCProviders: nil` was, so that is what this reads.
	if !names["tenant"] {
		t.Fatal("apps/platformkit/app.go composes no tenant.Module, which is the only module that puts the " +
			"authcontracts.OIDCProviders port the two OIDC legs are mounted on: this case would be reading a " +
			"composition with a need on it rather than the one the application runs")
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

// authDepsFields is the set of names the application's own file selects out of
// the auth package — which registration door it hands `Use`, and which doors it
// does not — and the set of modules it composes, both read out of its syntax
// tree rather than counted out of its text.
func authDepsFields(t *testing.T) map[string]bool {
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
	names := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || fn.Sel.Name != "Use" {
			return true
		}
		for _, arg := range call.Args {
			sel, ok := arg.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				continue
			}
			if pkg.Name == "auth" {
				names[sel.Sel.Name] = true
			}
			if pkg.Name == "tenant" {
				names["tenant"] = true
			}
		}
		return true
	})
	if len(names) == 0 {
		t.Fatal("apps/platformkit/app.go's Use list names nothing of the auth package: the composition moved, " +
			"and this case belongs beside wherever it went")
	}
	return names
}
