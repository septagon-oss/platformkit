package dbtest

import (
	"context"
	"strings"
	"testing"
)

func TestRolesKeepDistinctGrantsWhenSchemaNamesDifferOnlyAtTheLimit(t *testing.T) {
	adminURL, _ := URLs(t)
	admin := Open(t, adminURL)
	prefix := "long_" + DeploymentSchema(t, admin)
	prefix += strings.Repeat("x", 63)
	schemas := []string{prefix[:62] + "a", prefix[:62] + "b"}
	roles := make([]string, len(schemas))
	t.Run("granted roles", func(t *testing.T) {
		for index, schema := range schemas {
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
			owner := Open(t, target)
			if _, err := owner.ExecContext(t.Context(), "CREATE TABLE role_marker (value text NOT NULL)"); err != nil {
				t.Fatal(err)
			}
			if _, err := owner.ExecContext(t.Context(), "INSERT INTO role_marker VALUES ($1)", schema); err != nil {
				t.Fatal(err)
			}
			role, roleURL := Role(t, owner, "SELECT ON TABLE role_marker")
			roles[index] = role
			reader := Open(t, roleURL)
			var actualRole, actualSchema, value string
			if err := reader.QueryRowContext(t.Context(), "SELECT current_user, current_schema(), value FROM role_marker").Scan(&actualRole, &actualSchema, &value); err != nil {
				t.Fatal(err)
			}
			if actualRole != role || actualSchema != schema || value != schema {
				t.Errorf("role %q reached %q/%q holding %q; want its own schema %q", role, actualRole, actualSchema, value, schema)
			}
		}
		if roles[0] == roles[1] {
			t.Errorf("distinct schemas share role %q", roles[0])
		}
		for index, role := range roles {
			var allowed bool
			if err := admin.QueryRowContext(t.Context(), "SELECT has_schema_privilege($1, $2, 'USAGE')", role, schemas[1-index]).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if allowed {
				t.Errorf("role %q can use the other schema %q", role, schemas[1-index])
			}
		}
	})
	for _, role := range roles {
		if role == "" {
			t.Fatal("both grants must succeed before checking their cleanup")
		}
		var remains bool
		if err := admin.QueryRowContext(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", role).Scan(&remains); err != nil {
			t.Fatal(err)
		}
		if remains {
			t.Errorf("role %q remains after its grants ended", role)
		}
	}
}
