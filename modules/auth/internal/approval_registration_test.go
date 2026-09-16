package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

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

func approvalSignup(deps *auth.Deps) {
	deps.ApprovalRegistration = &contracts.ApprovalRegistration{
		Users: deps.Users.(contracts.PendingRegistrar), Roles: []string{"customer"},
	}
}

func approvalBody(t *testing.T, email string, edit func(map[string]any)) string {
	t.Helper()
	fields := map[string]any{"email": email, "displayName": " Pending Customer ", "password": authtest.Password, "confirmation": authtest.Password, "termsAccepted": true}
	if edit != nil {
		edit(fields)
	}
	body, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestApprovalSignupValidatesInputWithoutEchoingCredentials(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, approvalSignup, func(d *auth.Deps) {
		d.Mailer, d.Hosts = nil, nil
	})
	cases := []struct {
		field string
		value any
	}{
		{"password", "short"}, {"password", strings.Repeat("private", 50)},
		{"confirmation", "a different private passphrase"}, {"confirmation", nil},
		{"password", map[string]any{"secret": authtest.Password}},
		{"termsAccepted", false}, {"termsAccepted", nil},
		{"displayName", "   "}, {"email", "invalid"},
		{"roles", []string{"admin"}}, {"status", "active"},
	}
	for i, test := range cases {
		body := approvalBody(t, "pending@example.com", func(v map[string]any) {
			if test.value == nil {
				delete(v, test.field)
			} else {
				v[test.field] = test.value
			}
		})
		res := call(t, router, "POST", "/api/v1/auth/register", body, from("203.0.113."+strconv.Itoa(10+i)))
		if res.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid %s = %d", test.field, res.Code)
		}
		for _, secret := range []string{authtest.Password, "short", "a different private passphrase", strings.Repeat("private", 50)} {
			if strings.Contains(res.Body.String(), secret) {
				t.Fatal("validation echoed a credential")
			}
		}
	}
	var count int
	if err := admin.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid signup persisted a user: count=%d, err=%v", count, err)
	}
	if res := call(t, router, "POST", "/api/v1/auth/register", approvalBody(t, "pending@example.com", nil), func(r *http.Request) {
		r.Header.Set("Origin", "https://elsewhere.example")
	}); res.Code != http.StatusForbidden {
		t.Fatalf("cross-site signup = %d", res.Code)
	}
	res := call(t, router, "POST", "/api/v1/auth/register", approvalBody(t, "Pending@EXAMPLE.com", nil))
	if res.Code != http.StatusAccepted || len(res.Result().Cookies()) != 0 || strings.Contains(res.Body.String(), "pending") {
		t.Fatalf("registration without email delivery = %d", res.Code)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := realUsers().ByEmail(ctx, tx, "pending@example.com")
		if err == nil && (u.Status != user.StatusPending || u.DisplayName != "Pending Customer" || !slices.Equal(u.Roles, user.Roles{"customer"}) || !u.CheckPassword(authtest.Password)) {
			t.Fatal("signup lost its pending state, normalized name, trusted role or password")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestApprovalSignupPreservesExistingAccountsAndAcknowledgesDuplicates(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, approvalSignup)
	users := realUsers()
	var acknowledged string
	for i, status := range []string{user.StatusInvited, user.StatusPending, user.StatusActive, user.StatusInactive} {
		email := status + "@example.com"
		var original *user.User
		if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			u, err := users.Invite(ctx, tx, email, "Original")
			if err != nil {
				return err
			}
			if status != user.StatusInvited {
				if err := users.SetPassword(ctx, tx, u.ID, authtest.Password); err != nil {
					return err
				}
			}
			// Fixture lifecycle states; the public route must never alter any of them.
			if err := tx.DB().Exec("UPDATE users SET status = ? WHERE id = ?", status, u.ID).Error; err != nil {
				return err
			}
			original, err = users.SetRoles(ctx, tx, u.ID, []string{"admin"})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		res := call(t, router, "POST", "/api/v1/auth/register", approvalBody(t, strings.ToUpper(email), func(v map[string]any) {
			v["password"], v["confirmation"] = "replacement private password", "replacement private password"
		}), from("203.0.113."+strconv.Itoa(10+i)))
		if res.Code != http.StatusAccepted {
			t.Fatalf("existing %s registration = %d", status, res.Code)
		}
		if acknowledged != "" && acknowledged != res.Body.String() {
			t.Fatal("acknowledgment exposed an account's lifecycle state")
		}
		acknowledged = res.Body.String()
		if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			got, err := users.Get(ctx, tx, original.ID)
			if err == nil && (got.Status != original.Status || got.DisplayName != original.DisplayName || got.PasswordHash != original.PasswordHash || !slices.Equal(got.Roles, original.Roles)) {
				t.Fatal("repeat signup replaced an existing account")
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	fresh := call(t, router, "POST", "/api/v1/auth/register", approvalBody(t, "fresh@example.com", nil))
	if fresh.Code != http.StatusAccepted || fresh.Body.String() != acknowledged {
		t.Fatal("fresh and existing accounts received different acknowledgments")
	}
}

func TestConcurrentApprovalSignupsCreateOnePendingAccountAndNoCredentialEvent(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, approvalSignup)
	body := approvalBody(t, "race@example.com", nil)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if res := call(t, router, "POST", "/api/v1/auth/register", body); res.Code != http.StatusAccepted {
				t.Errorf("concurrent signup = %d", res.Code)
			}
		})
	}
	wg.Wait()
	var count int
	if err := admin.QueryRow("SELECT count(*) FROM users WHERE email = 'race@example.com' AND status = 'pending'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("concurrent signup count=%d, err=%v", count, err)
	}
	var payloads []string
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("platformkit_outbox").Pluck("payload", &payloads).Error
	}); err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || strings.Contains(payloads[0], authtest.Password) || strings.Contains(payloads[0], "argon2id") {
		t.Fatal("signup published a duplicate event or queued credentials")
	}
	worker(t, conn)
	if len(mailbox.Sent()) != 0 {
		t.Fatal("pending signup sent a password-activation email")
	}
}

func TestApprovalSignupSharesTheRecoveryLimitAndRollsBackFailedEvents(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, approvalSignup)
	for range contracts.ResetRequests {
		if res := call(t, router, "POST", "/api/v1/auth/password/forgot", `{"email":"missing@example.com"}`, from("203.0.113.8")); res.Code != 200 {
			t.Fatalf("recovery = %d", res.Code)
		}
	}
	body := approvalBody(t, "pending@example.com", nil)
	if res := call(t, router, "POST", "/api/v1/auth/register", body, from("203.0.113.8")); res.Code != 429 {
		t.Fatalf("shared signup/recovery limit = %d", res.Code)
	}
	if _, err := admin.Exec("ALTER TABLE platformkit_outbox ADD CONSTRAINT refuse_pending_test CHECK (name <> 'user.registration_pending')"); err != nil {
		t.Fatal(err)
	}
	if res := call(t, router, "POST", "/api/v1/auth/register", body); res.Code != 500 {
		t.Fatalf("failed account event = %d", res.Code)
	}
	var count int
	if err := admin.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil || count != 0 {
		t.Fatalf("refused or failed signup left an account: %d, %v", count, err)
	}
}

func TestApprovalSignupDoesNotAcknowledgeAnUnrelatedDatabaseConflict(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, approvalSignup)
	// A fixture-only constraint simulates an unrelated storage conflict. Only
	// the user owner's email-exists result may become a neutral acknowledgment.
	if _, err := admin.Exec("CREATE UNIQUE INDEX unrelated_signup_conflict ON users (display_name)"); err != nil {
		t.Fatal(err)
	}
	for i, email := range []string{"first@example.com", "second@example.com"} {
		res := call(t, router, "POST", "/api/v1/auth/register", approvalBody(t, email, nil))
		want := http.StatusAccepted
		if i == 1 {
			want = http.StatusConflict
		}
		if res.Code != want {
			t.Fatalf("registration %d = %d, want %d", i, res.Code, want)
		}
	}
	var count int
	if err := admin.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil || count != 1 {
		t.Fatalf("unrelated conflict persisted another account: %d, %v", count, err)
	}
}

func TestApprovalSignupRefusesAmbiguousOrInvalidComposition(t *testing.T) {
	for _, configure := range []func(*auth.Deps){
		func(d *auth.Deps) { d.Registration = realUsers() },
		func(d *auth.Deps) { d.ApprovalRegistration.Users = nil },
		func(d *auth.Deps) { d.ApprovalRegistration.Roles = nil },
		func(d *auth.Deps) { d.ApprovalRegistration.Roles = []string{"invalid-role"} },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("invalid registration composition was accepted")
				}
			}()
			deps := auth.Deps{Users: realUsers()}
			approvalSignup(&deps)
			configure(&deps)
			auth.Module(deps)
		}()
	}
}
