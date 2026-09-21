package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestPendingRegistrationCannotRecoverOrSignInBeforeApproval(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notification.Migrations, auth.Migrations)
	router, _, auths := mountConfigured(t, conn, auth.OIDC{}, false, approvalSignup)
	users := realUsers()
	var pending *user.User
	if res := call(t, router, "POST", "/api/v1/public/auth/register", approvalBody(t, "pending@example.com", nil)); res.Code != http.StatusAccepted {
		t.Fatalf("public registration = %d", res.Code)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		pending, err = users.ByEmail(ctx, tx, "pending@example.com")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	login := `{"email":"pending@example.com","password":"` + authtest.Password + `"}`
	if res := call(t, router, "POST", "/api/v1/auth/login", login); res.Code != 401 {
		t.Fatalf("pending login = %d", res.Code)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, _, err := auths.Open(ctx, tx, pending.ID, contracts.Client{}); !errors.Is(err, contracts.ErrCredentials) {
			t.Fatalf("pending OIDC session = %v", err)
		}
		if err := auths.Forget(ctx, tx, pending.Email); err != nil {
			return err
		}
		return auths.Offer(ctx, tx, pending.ID)
	}); err != nil {
		t.Fatal(err)
	}
	worker(t, conn)
	if len(mailbox.Sent()) != 0 {
		t.Fatal("pending registration received an activation or reset link")
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var tokens int64
		if err := tx.DB().Table("password_tokens").Where("user_id = ?", pending.ID).Count(&tokens).Error; err != nil {
			return err
		}
		if tokens != 0 {
			t.Fatal("pending account has a live recovery credential")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/user/users/" + pending.ID.String() + "/approve-registration"
	if res := call(t, router, "POST", path, `{}`); res.Code != 403 {
		t.Fatalf("anonymous approval = %d", res.Code)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := auths.SetRole(ctx, tx, "approver", []string{user.PermissionRegistrationApprove}, []tenancy.Grant{{Permission: user.PermissionRegistrationApprove}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	operator := person(t, conn, "operator@example.com", "approver")
	signed := call(t, router, "POST", "/api/v1/auth/login", `{"email":"operator@example.com","password":"`+authtest.Password+`"}`)
	if signed.Code != 200 {
		t.Fatal("operator could not sign in")
	}
	cookie := signed.Result().Cookies()[0]
	for _, forbidden := range []string{"roles", "set-password"} {
		res := call(t, router, "POST", "/api/v1/user/users/"+pending.ID.String()+"/"+forbidden, `{}`, func(r *http.Request) { r.AddCookie(cookie) })
		if res.Code != 403 {
			t.Fatalf("approval-only account can call %s: %d", forbidden, res.Code)
		}
	}
	for range 2 {
		res := call(t, router, "POST", path, `{}`, func(r *http.Request) { r.AddCookie(cookie) })
		if res.Code != 200 {
			t.Fatalf("approval = %d", res.Code)
		}
		if strings.Contains(res.Body.String(), "argon2id") || strings.Contains(res.Body.String(), authtest.Password) {
			t.Fatal("approval exposed credentials")
		}
	}
	if res := call(t, router, "POST", "/api/v1/auth/login", login); res.Code != 200 {
		t.Fatalf("approved login = %d", res.Code)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var payloads []string
		if err := tx.DB().Table("platformkit_outbox").Where("name = ?", user.EventRegistrationApproved).Pluck("payload", &payloads).Error; err != nil {
			return err
		}
		if len(payloads) != 1 {
			t.Fatalf("approval events = %d", len(payloads))
		}
		var event user.RegistrationApproved
		if err := json.Unmarshal([]byte(payloads[0]), &event); err != nil {
			return err
		}
		if event.UserID != pending.ID || event.Actor != operator || event.At.IsZero() {
			t.Fatal("approval lost its actor, subject or time")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPendingRegistrationRefusesEvenAPreexistingPasswordToken(t *testing.T) {
	_, conn, auths := mount(t, auth.OIDC{})
	users := realUsers()
	var id uuid.UUID
	const token = "disposable-pending-registration-token"
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := users.RegisterPending(ctx, tx, user.PendingRegistration{Email: "pending@example.com", Password: authtest.Password})
		if err != nil {
			return err
		}
		id = u.ID
		return tx.DB().Exec("INSERT INTO password_tokens (token_hash, tenant_id, user_id, expires_at) VALUES (?, ?, ?, now() + interval '1 hour')", contracts.Hash(token), acme.ID, id).Error
	}); err != nil {
		t.Fatal(err)
	}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return auths.Reset(ctx, tx, token, authtest.Password+" changed")
	})
	if !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("pending password token = %v", err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := users.Get(ctx, tx, id)
		if err == nil && (u.Status != user.StatusPending || !u.CheckPassword(authtest.Password)) {
			t.Fatal("reset bypassed pending approval")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
