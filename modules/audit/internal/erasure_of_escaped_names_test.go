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
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

func TestErasureRemovesNamesThatJSONEscapes(t *testing.T) {
	for _, name := range []string{`Jo "JJ" Jones`, `Jo\Jones`} {
		t.Run(name, func(t *testing.T) {
			_, conn := dbtest.Schema(t, audit.Migrations)
			ctx := tenancy.WithTenant(t.Context(), acme)
			person := personaldata.Subject{ID: uuid.New(), Email: "escaped@example.com", DisplayName: name}
			id, event := uuid.New(), uuid.New()
			payload, err := json.Marshal(map[string]any{"displayName": name, "active": true})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				return tx.DB().Exec(`INSERT INTO audit_events
					(id, tenant_id, occurred_at, name, actor, event_id, payload)
					VALUES (?, ?, now(), 'user.updated', ?, ?, ?::jsonb)`,
					id, acme.ID, person.ID, event, string(payload)).Error
			}); err != nil {
				t.Fatal(err)
			}
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return internal.PersonalData().Data.Erase(ctx, tx, person)
			}); err != nil {
				t.Fatalf("erasure: %v", err)
			}
			var row struct {
				Name        string
				Actor       uuid.UUID
				EventID     uuid.UUID
				DisplayName string
				Active      bool
			}
			if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				return tx.DB().Raw(`SELECT name, actor, event_id,
					payload->>'displayName' AS display_name,
					(payload->>'active')::boolean AS active FROM audit_events WHERE id = ?`, id).Scan(&row).Error
			}); err != nil {
				t.Fatal(err)
			}
			if row.Actor != person.ID || row.EventID != event || row.Name != "user.updated" || !row.Active {
				t.Errorf("erasure changed the audit facts: %+v", row)
			}
			if row.DisplayName != person.Token() {
				t.Errorf("audit displayName after erasure = %q, want %q", row.DisplayName, person.Token())
			}
		})
	}
}
