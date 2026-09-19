package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestEmailVerificationKeepsTenantOwnershipAndPublishesAtomically(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations)
	svc := newService()
	const password = "correct horse battery staple"
	if _, err := admin.ExecContext(t.Context(), "ALTER TABLE platformkit_outbox ADD CONSTRAINT reject_registration_event CHECK (name <> 'user.registration_unverified')"); err != nil {
		t.Fatal(err)
	}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.RegisterUnverified(ctx, tx, contracts.PasswordRegistration{Email: "failed@example.com", Password: password})
		return err
	})
	if err == nil {
		t.Fatal("registration succeeded without its durable event")
	}
	var count int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM users").Scan(&count); err != nil || count != 0 {
		t.Fatal("event failure left a registered account behind")
	}
	if _, err := admin.ExecContext(t.Context(), "ALTER TABLE platformkit_outbox DROP CONSTRAINT reject_registration_event"); err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for _, tenant := range []tenancy.Tenant{acme, globex} {
		if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			u, err := svc.RegisterUnverified(ctx, tx, contracts.PasswordRegistration{Email: "same@example.com", Password: password, Roles: []string{"customer"}})
			if err == nil {
				ids = append(ids, u.ID)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := admin.ExecContext(t.Context(), "ALTER TABLE platformkit_outbox ADD CONSTRAINT reject_verification_event CHECK (name <> 'user.email_verified')"); err != nil {
		t.Fatal(err)
	}
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.VerifyEmail(ctx, tx, ids[0], "same@example.com")
		return err
	})
	if err == nil {
		t.Fatal("verification succeeded without its durable event")
	}
	var pending int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM users WHERE status = 'unverified'").Scan(&pending); err != nil || pending != 2 {
		t.Fatalf("event failure did not roll activation back: count=%d, error=%v", pending, err)
	}
	if _, err := admin.ExecContext(t.Context(), "ALTER TABLE platformkit_outbox DROP CONSTRAINT reject_verification_event"); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.VerifyEmail(ctx, tx, ids[1], "same@example.com"); !errors.Is(err, crud.ErrNotFound) {
			t.Fatalf("cross-tenant verification = %v", err)
		}
		_, err := svc.VerifyEmail(ctx, tx, ids[0], "same@example.com")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := admin.QueryContext(t.Context(), "SELECT name, payload::text FROM platformkit_outbox ORDER BY created_at, id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var name, raw string
		if err := rows.Scan(&name, &raw); err != nil {
			t.Fatal(err)
		}
		counts[name]++
		var payload map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &payload); err != nil || len(payload) != 3 || strings.Contains(raw, password) || strings.Contains(raw, "argon2") {
			t.Fatal("registration event contains credentials or unexpected fields")
		}
		var id uuid.UUID
		var email string
		var at time.Time
		if json.Unmarshal(payload["userId"], &id) != nil || json.Unmarshal(payload["email"], &email) != nil || json.Unmarshal(payload["at"], &at) != nil || email != "same@example.com" || at.IsZero() || (id != ids[0] && id != ids[1]) {
			t.Fatal("registration event lost its canonical user, email or time")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if counts[contracts.EventRegistrationUnverified] != 2 || counts[contracts.EventEmailVerified] != 1 || len(counts) != 2 {
		t.Fatal("registration or verification published an extra or missing event")
	}
}

func TestEmailVerificationRefusesMissingPasswordAndDeletedUsers(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	svc := newService()
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := svc.RegisterUnverified(ctx, tx, contracts.PasswordRegistration{Email: "ada@example.com", Password: "correct horse battery staple"})
		if err != nil {
			return err
		}
		if err := tx.DB().Model(u).Update("password_hash", "").Error; err != nil {
			return err
		}
		if _, err := svc.VerifyEmail(ctx, tx, u.ID, u.Email); !errors.Is(err, crud.ErrConflict) {
			t.Fatalf("verification without a password = %v", err)
		}
		if err := tx.DB().Delete(u).Error; err != nil {
			return err
		}
		if _, err := svc.VerifyEmail(ctx, tx, u.ID, u.Email); !errors.Is(err, crud.ErrNotFound) {
			t.Fatalf("verification of a deleted user = %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
