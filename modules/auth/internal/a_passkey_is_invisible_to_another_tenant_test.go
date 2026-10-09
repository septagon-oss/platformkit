package internal_test

// A passkey enrolled at one tenant is a row the other tenant cannot see, write
// or switch on: the three passkey tables are row-level secured, and this case
// asks the database rather than the code that happens to filter by tenant.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestAPasskeyIsInvisibleToAnotherTenant(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	seed(t, conn, globex)
	ada := person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	if code, body := enrolPasskey(t, router, session, newSoftAuthenticator(t, host)); code != http.StatusCreated {
		t.Fatalf("enrol a passkey at acme = %d %s, want 201", code, body)
	}
	setPasskeySignIn(t, conn, true)
	if res := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", ""); res.Code != http.StatusOK {
		t.Fatalf("begin a passkey sign-in at acme = %d %s", res.Code, res.Body.String())
	}

	count := func(tenant tenancy.Tenant) map[string]int64 {
		t.Helper()
		seen := map[string]int64{}
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			for _, table := range []string{"passkey_credentials", "passkey_challenges", "passkey_settings"} {
				var n int64
				if err := tx.DB().Table(table).Count(&n).Error; err != nil {
					return err
				}
				seen[table] = n
			}
			return nil
		})
		if err != nil {
			t.Fatalf("read the passkey tables as %s: %v", tenant.Slug, err)
		}
		return seen
	}
	for table, n := range count(acme) {
		if n == 0 {
			t.Fatalf("acme holds no row in %s: the case below would prove nothing", table)
		}
	}
	for table, n := range count(globex) {
		if n != 0 {
			t.Errorf("globex sees %d of acme's rows in %s, want 0", n, table)
		}
	}

	// Writing a row in acme's name from globex's transaction is refused by the policy.
	err := db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(
			"INSERT INTO passkey_credentials (id, tenant_id, user_id, credential_id, public_key) VALUES (?, ?, ?, ?, ?)",
			uuid.New(), acme.ID, ada, []byte{1}, []byte{1}).Error
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Errorf("globex writing a passkey in acme's name = %v, want the row-level security policy's refusal", err)
	}
}
