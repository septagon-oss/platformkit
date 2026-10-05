package internal_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

// TestRetentionDoesNotEchoItsRolesPassword: the expiry role's DSN carries a password,
// and the job's refusal to reach it names the job, not the secret.
func TestRetentionDoesNotEchoItsRolesPassword(t *testing.T) {
	const dsn = "postgres://platformkit_retain:hunter2secret@127.0.0.1:1/platformkit?sslmode=disable&connect_timeout=2"
	err := internal.Retention(lister{acme}, 365, dsn).Run(t.Context(), nil)
	if err == nil {
		t.Fatal("the retention job reached a database at port 1")
	}
	if !strings.Contains(err.Error(), "audit:") {
		t.Errorf("the refusal does not say whose it is: %v", err)
	}
	if strings.Contains(err.Error(), "hunter2secret") {
		t.Errorf("the refusal echoes the password: %v", err)
	}
}
