package dbtest

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestRoleConnectsAtEveryUnicodeCutPositionAndLeavesNoLogin(t *testing.T) {
	adminURL, _ := URLs(t)
	admin := Open(t, adminURL)
	for _, character := range []string{"é", "界", "𐐀"} {
		for offset := range len(character) {
			t.Run(fmt.Sprintf("%d bytes/offset %d", len(character), offset), func(t *testing.T) {
				prefix := fmt.Sprintf("wide_%s_%d_%d_", run, len(character), offset)
				// The derived role's 54-byte prefix ends at each byte of this character.
				schema := prefix + strings.Repeat("x", 52-offset-len(prefix)) + character
				schema += strings.Repeat("z", 63-len(schema))
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
				t.Run("connected", func(t *testing.T) {
					owner := Open(t, target)
					var roleURL string
					role, roleURL = Role(t, owner)
					reader := Open(t, roleURL)
					var actualRole, actualSchema string
					if err := reader.QueryRowContext(t.Context(), "SELECT current_user, current_schema()").Scan(&actualRole, &actualSchema); err != nil {
						t.Fatal(err)
					}
					if actualRole != role || actualSchema != schema {
						t.Errorf("login %q reached role %q and schema %q, want schema %q", role, actualRole, actualSchema, schema)
					}
				})
				if role == "" {
					t.Fatal("the namespace must yield a usable login before checking its cleanup")
				}
				var remains bool
				if err := admin.QueryRowContext(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", role).Scan(&remains); err != nil {
					t.Fatal(err)
				}
				if remains {
					t.Errorf("role %q remains after its connection ended", role)
				}
			})
		}
	}
}
