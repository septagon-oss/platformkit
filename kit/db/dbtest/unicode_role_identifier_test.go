package dbtest

import (
	"context"
	"strings"
	"testing"
)

func TestRoleForAMaximumLengthUnicodeSchemaCanConnectAndIsRemoved(t *testing.T) {
	adminURL, _ := URLs(t)
	admin := Open(t, adminURL)
	prefix := "locale_" + run
	if len(prefix)%2 == 0 {
		prefix += "x"
	}
	schema := prefix + strings.Repeat("é", (63-len(prefix))/2)
	if _, err := admin.ExecContext(t.Context(), "CREATE SCHEMA "+quote(schema)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.WithoutCancel(t.Context()), "DROP SCHEMA "+quote(schema)+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	target, err := schemaURL(adminURL, schema)
	if err != nil {
		t.Fatal(err)
	}
	var role string
	t.Run("granted connection", func(t *testing.T) {
		owner := Open(t, target)
		var roleURL string
		role, roleURL = Role(t, owner)
		reader := Open(t, roleURL)
		var gotRole, gotSchema string
		if err := reader.QueryRowContext(t.Context(), "SELECT current_user, current_schema()").Scan(&gotRole, &gotSchema); err != nil {
			t.Fatal(err)
		}
		if gotRole != role || gotSchema != schema {
			t.Errorf("role %q reached %q in schema %q, want %q", role, gotRole, gotSchema, schema)
		}
	})
	if role == "" {
		return
	}
	var remains bool
	if err := admin.QueryRowContext(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", role).Scan(&remains); err != nil {
		t.Fatal(err)
	}
	if remains {
		t.Errorf("role %q remains after its connection ended", role)
	}
}
