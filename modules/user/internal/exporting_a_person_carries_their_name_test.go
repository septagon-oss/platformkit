package internal_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/user/internal"
)

// TestExportingAPersonCarriesTheirName exports a person whose row has a display name:
// the copy Article 15 owes them holds the name the row holds, and the erasure that
// follows leaves neither the name nor the address in the row.
func TestExportingAPersonCarriesTheirName(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	in := tenancy.WithTenant(t.Context(), acme)
	subject := personaldata.Subject{ID: uuid.New(), Email: "jo.jones@example.com", DisplayName: "Jo Jones", Handle: "jjones"}
	part := internal.PersonalData(newService())

	if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(`INSERT INTO users (id, tenant_id, email, display_name, handle) VALUES (?, ?, ?, ?, ?)`,
			subject.ID, acme.ID, subject.Email, subject.DisplayName, subject.Handle).Error
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var exported json.RawMessage
	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		exported, err = part.Data.Export(ctx, tx, subject)
		return err
	}); err != nil {
		t.Fatalf("export: %v", err)
	}
	var got struct {
		Users []map[string]any `json:"users"`
	}
	if err := json.Unmarshal(exported, &got); err != nil || len(got.Users) != 1 {
		t.Fatalf("the part is not one user row: %s (%v)", exported, err)
	}
	if got.Users[0]["displayName"] != subject.DisplayName {
		t.Errorf("displayName = %v, want %q: %s", got.Users[0]["displayName"], subject.DisplayName, exported)
	}

	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return part.Data.Erase(ctx, tx, subject)
	}); err != nil {
		t.Fatalf("erase: %v", err)
	}
	var left int
	if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*) FROM users WHERE id = ? AND (email = ? OR display_name = ? OR handle = ?)`,
			subject.ID, subject.Email, subject.DisplayName, subject.Handle).Scan(&left).Error
	}); err != nil || left != 0 {
		t.Errorf("the erased row still names the person: %d (%v)", left, err)
	}
}
