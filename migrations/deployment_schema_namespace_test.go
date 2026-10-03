package migrations_test

// The fourth review's case for dbtest.DeploymentSchema, the one name this change
// made reusable. Its doc comment promises a resolved answer — "the answer has to be
// what the database resolved, not what this package meant"
// (kit/db/dbtest/dbtest.go:117-120) — and a shape it guarantees, "t_<run>_<test>",
// "the shape an owner name is cut from" — which is how moduleOwner and the third
// review's file derive every owner and role name in this package. What it reads is
// current_setting('search_path'): the raw setting the connection URL asked the
// server to place, echoed back unchanged, and that setting is a *list*.
// current_schema() is the namespace the server resolved that list to, and the one an
// unqualified CREATE TABLE lands in — which is what "the schema this package made"
// is supposed to name.
//
// It matters because the helper is exported for callers outside this package, and the
// guard is two bytes: a path of "t_x,public" passes it, and a caller that cuts a
// module owner or a role name out of the return value then writes a comma into the
// middle of an identifier. The reference deployment answers `"$user", public` for the
// same GUC where current_schema() answers public, and the shipped function's own
// header reads the resolved name, not this one, to make exactly that point
// (migrations/000026_module_schema.up.sql:57-63): "a session that names no path at
// all resolves to public, because an unset search_path is "$user", public".
//
// The assertion is the namespace this session's unqualified DDL lands in, asked of
// the server in the same breath, so the test fails over a wrong name and not over a
// message only a broken helper could print. Reading current_schema() instead is
// enough: every dbtest URL names exactly one schema today, so both answers agree for
// every caller that exists, and go test ./migrations/... passes unchanged with that
// one line in place of the current one (measured, this review).

import (
	"context"
	"database/sql"
	"net/url"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestDeploymentSchemaNamesTheNamespaceTheSessionLandsIn(t *testing.T) {
	ctx := t.Context()
	adminURL, _ := dbtest.URLs(t)
	admin := dbtest.Open(t, adminURL)
	made := dbtest.DeploymentSchema(t, admin)

	// A second handle on that same schema whose path names a second namespace as
	// well, the way an installation's default path names "$user" and public. Every
	// connection this handle opens carries the pair, so the answer cannot depend on
	// which connection the pool happens to hand over.
	paired := dbtest.Open(t, withSecondName(t, adminURL, made))
	if got := resolvedNamespace(t, ctx, paired); got != made {
		t.Fatalf("the premise is broken: this session resolves to %q, not to %q, "+
			"so the answer below would measure the wrong namespace", got, made)
	}

	if got := dbtest.DeploymentSchema(t, paired); got != made {
		t.Errorf("dbtest.DeploymentSchema returned %q for a session whose search_path resolves to %q. "+
			"The name is what a caller cuts a module owner and its role names from, the guard admits "+
			"anything that starts t_, and %q is not a schema: it is the setting, with a second "+
			"namespace in it.", got, made, got)
	}
}

// resolvedNamespace is the namespace the server put this session's unqualified names
// in — the answer DeploymentSchema documents that it gives.
func resolvedNamespace(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var schema string
	if err := db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatalf("ask which namespace this session resolves to: %v", err)
	}
	return schema
}

// withSecondName is a dbtest URL with public named after its own schema, the way a
// default search_path names more than one namespace.
func withSecondName(t *testing.T, rawURL, schema string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse the admin URL: %v", err)
	}
	q := u.Query()
	q.Set("options", "-csearch_path="+schema+",public")
	u.RawQuery = q.Encode()
	return u.String()
}
