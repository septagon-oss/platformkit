package internal_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// TestErasingAPersonRemovesTheirCredentials runs auth's personal-data part against the
// tables the module's own migrations create: a person with a pending address
// verification exports it, and after the erasure no verification token of theirs —
// and so no copy of their address in it — is left in the tenant.
func TestErasingAPersonRemovesTheirCredentials(t *testing.T) {
	const email = "jo.jones@example.com"
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	person := uuid.New()
	subject := personaldata.Subject{ID: person, Email: email, DisplayName: "Jo Jones", Handle: "jjones"}
	in := tenancy.WithTenant(t.Context(), acme)

	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Exec(`INSERT INTO users (id, tenant_id, email, display_name) VALUES (?, ?, ?, ?)`,
			person, acme.ID, email, "Jo Jones").Error; err != nil {
			return err
		}
		return tx.DB().Exec(`INSERT INTO verification_tokens (token_hash, tenant_id, user_id, email, expires_at)
			VALUES (sha256('a link'::bytea), ?, ?, ?, ?)`, acme.ID, person, email,
			time.Now().UTC().Add(time.Hour)).Error
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var part json.RawMessage
	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		part, err = internal.PersonalData().Data.Export(ctx, tx, subject)
		return err
	}); err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(string(part), email) {
		t.Errorf("the part does not carry the address the pending verification was sent to: %s", part)
	}

	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, subject)
	}); err != nil {
		t.Fatalf("erase: %v", err)
	}

	var left int
	if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*) FROM verification_tokens WHERE user_id = ? OR lower(email) = lower(?)`,
			person, email).Scan(&left).Error
	}); err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if left != 0 {
		t.Errorf("%d verification tokens still name the erased person", left)
	}
}
