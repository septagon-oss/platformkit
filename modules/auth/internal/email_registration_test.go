package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func emailSignup(deps *auth.Deps) {
	deps.EmailRegistration = &contracts.EmailRegistration{Users: deps.Users.(contracts.EmailRegistrar), Roles: []string{"member"}}
}

func verificationBody(t *testing.T, token string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		t.Fatal("encode verification request")
	}
	return string(body)
}

func verificationSignup(t *testing.T, conn *db.Conn, router chi.Router, email string) (*user.User, string) {
	t.Helper()
	before := len(mailbox.Sent())
	res := call(t, router, "POST", "/api/v1/auth/register", approvalBody(t, email, nil))
	if res.Code != http.StatusAccepted || len(res.Result().Cookies()) != 0 {
		t.Fatalf("email signup status=%d cookies=%d", res.Code, len(res.Result().Cookies()))
	}
	if len(mailbox.Sent()) != before {
		t.Fatal("signup sent mail inside its public transaction")
	}
	worker(t, conn)
	letters := mailbox.Sent()
	if len(letters) != before+1 || letters[before].To != contracts.EmailKey(email) || !strings.Contains(letters[before].Body, "http://"+host+contracts.VerifyEmailPath+"?token=") {
		t.Fatal("worker did not send one verification link on this tenant's host")
	}
	token := authtest.TokenIn(letters[before].Body)
	if len(token) != 43 {
		t.Fatal("verification link did not carry a 256-bit base64url token")
	}
	var account *user.User
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		account, err = realUsers().ByEmail(ctx, tx, email)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return account, token
}

func TestEmailSignupKeepsThePasswordAndRequiresExplicitVerification(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup)
	u, token := verificationSignup(t, conn, router, "New@EXAMPLE.com")
	if u.Status != user.StatusUnverified || u.CanSignIn() || !u.CheckPassword(authtest.Password) || !slices.Equal(u.Roles, user.Roles{"member"}) {
		t.Fatal("signup lost its verification gate, registered password or trusted role")
	}
	var lifetime float64
	if err := admin.QueryRow("SELECT extract(epoch FROM expires_at-created_at) FROM verification_tokens WHERE user_id=$1", u.ID).Scan(&lifetime); err != nil || lifetime != (24*time.Hour).Seconds() {
		t.Fatalf("verification lifetime=%v err=%v", lifetime, err)
	}
	login := `{"email":"new@example.com","password":"` + authtest.Password + `"}`
	if res := call(t, router, "POST", "/api/v1/auth/login", login); res.Code != http.StatusUnauthorized {
		t.Fatalf("unverified password login=%d", res.Code)
	}
	for _, method := range []string{"GET", "HEAD"} {
		res := call(t, router, method, "/api/v1/auth/verify-email?token="+token, "")
		if res.Code != http.StatusMethodNotAllowed && res.Code != http.StatusNotFound {
			t.Fatalf("verification %s unexpectedly mounted: %d", method, res.Code)
		}
	}
	res := call(t, router, "POST", "/api/v1/auth/verify-email", verificationBody(t, token))
	if res.Code != http.StatusOK || len(res.Result().Cookies()) != 0 {
		t.Fatalf("verification status=%d cookies=%d", res.Code, len(res.Result().Cookies()))
	}
	if res := call(t, router, "GET", "/api/v1/auth/me", ""); res.Code != http.StatusForbidden {
		t.Fatalf("verification established a session: %d", res.Code)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		got, err := realUsers().Get(ctx, tx, u.ID)
		if err == nil && (!got.CanSignIn() || got.PasswordHash != u.PasswordHash || !slices.Equal(got.Roles, u.Roles)) {
			t.Fatal("verification changed credentials or roles")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if res := call(t, router, "POST", "/api/v1/auth/verify-email", verificationBody(t, token)); res.Code != http.StatusUnauthorized {
		t.Fatalf("verification replay=%d", res.Code)
	}
	if res := call(t, router, "POST", "/api/v1/auth/login", login); res.Code != http.StatusOK {
		t.Fatalf("registered password after verification=%d", res.Code)
	}
}

func TestEmailSignupValidatesConsentCredentialsAndComposition(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup)
	cases := []struct {
		field string
		value any
	}{
		{"password", "short"}, {"confirmation", "a different private password"},
		{"termsAccepted", false}, {"displayName", "   "}, {"email", "invalid"},
		{"roles", []string{"admin"}}, {"status", "active"}, {"password", strings.Repeat("secret", 50)},
	}
	for i, item := range cases {
		body := approvalBody(t, "new@example.com", func(fields map[string]any) { fields[item.field] = item.value })
		res := call(t, router, "POST", "/api/v1/auth/register", body, from("203.0.113."+strconv.Itoa(i+10)))
		if res.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid %s status=%d", item.field, res.Code)
		}
		if strings.Contains(res.Body.String(), authtest.Password) || strings.Contains(res.Body.String(), "a different private password") || strings.Contains(res.Body.String(), strings.Repeat("secret", 50)) {
			t.Fatal("signup validation echoed a password")
		}
	}
	for _, path := range []string{"register", "resend-verification", "verify-email"} {
		body := `{"email":"new@example.com"}`
		if path == "register" {
			body = approvalBody(t, "new@example.com", nil)
		}
		if path == "verify-email" {
			body = verificationBody(t, "not-a-real-credential")
		}
		res := call(t, router, "POST", "/api/v1/auth/"+path, body, func(r *http.Request) { r.Header.Set("Origin", "https://elsewhere.example") })
		if res.Code != http.StatusForbidden {
			t.Fatalf("cross-site %s=%d", path, res.Code)
		}
	}
	var count int
	if err := admin.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid input persisted accounts: %d, %v", count, err)
	}
	for _, change := range []func(*auth.Deps){
		func(d *auth.Deps) { d.Mailer = nil }, func(d *auth.Deps) { d.Hosts = nil },
	} {
		router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, change)
		for _, path := range []string{"register", "resend-verification"} {
			body := `{"email":"unavailable@example.com"}`
			if path == "register" {
				body = approvalBody(t, "unavailable@example.com", nil)
			}
			if res := call(t, router, "POST", "/api/v1/auth/"+path, body); res.Code != http.StatusServiceUnavailable {
				t.Fatalf("%s without delivery=%d", path, res.Code)
			}
		}
	}
	for _, change := range []func(*auth.Deps){
		func(d *auth.Deps) { d.Registration = realUsers() }, approvalSignup,
		func(d *auth.Deps) { d.EmailRegistration.Users = nil },
		func(d *auth.Deps) { d.EmailRegistration.Roles = nil },
		func(d *auth.Deps) { d.EmailRegistration.Roles = []string{"invalid-role"} },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("invalid email registration policy accepted")
				}
			}()
			deps := auth.Deps{Users: realUsers()}
			emailSignup(&deps)
			change(&deps)
			auth.Module(deps)
		}()
	}
}

type verificationSites map[string]tenancy.Tenant

func (sites verificationSites) ByHost(_ context.Context, _ db.Tx[db.System], host string) (tenancy.Tenant, error) {
	if tenant, ok := sites[host]; ok {
		return tenant, nil
	}
	return tenancy.Tenant{}, tenancy.ErrNoSuchHost
}

func TestEmailVerificationCannotCrossTenantHosts(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup)
	_, token := verificationSignup(t, conn, router, "tenant@example.com")
	seed(t, conn, globex)
	deps := auth.Deps{Users: realUsers(), Mailer: mailbox, Hosts: authtest.Host(host), PublicHost: host}
	emailSignup(&deps)
	svc, mod := auth.Module(deps)
	api, tenants := httpx.New(httpx.Options{
		PublicHost: host, Conn: conn, Authorize: svc, Authenticate: svc.Authenticate,
		Tenants: verificationSites{host: acme, "globex.localhost": globex}, Log: slog.New(slog.DiscardHandler),
	})
	api.Declare([]tenancy.Grant{{Permission: contracts.PermissionRoleManage}})
	mod.Routes(api)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	res := call(t, tenants, "POST", "/api/v1/auth/verify-email", verificationBody(t, token), func(r *http.Request) { r.Host = "globex.localhost" })
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("foreign host verification=%d", res.Code)
	}
	if res := call(t, tenants, "POST", "/api/v1/auth/verify-email", verificationBody(t, token)); res.Code != http.StatusOK {
		t.Fatalf("foreign attempt consumed the issuing tenant's credential: %d", res.Code)
	}
}

func TestPasswordTokenCannotConfirmEmailOrActivateUnverifiedAccounts(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	router, _, auths := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup)
	id := person(t, conn, "recovery@example.com")
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return auths.Reissue(ctx, tx, "recovery@example.com")
	}); err != nil {
		t.Fatal(err)
	}
	letters := mailbox.Sent()
	if len(letters) != 1 {
		t.Fatal("active account did not receive its password reset")
	}
	token := authtest.TokenIn(letters[0].Body)
	if token == "" {
		t.Fatal("password reset mail had no credential")
	}
	if res := call(t, router, "POST", "/api/v1/auth/verify-email", verificationBody(t, token)); res.Code != http.StatusUnauthorized {
		t.Fatalf("password credential used for email verification=%d", res.Code)
	}
	// Simulate a previously issued credential surviving an account-state change.
	if _, err := admin.Exec("UPDATE users SET status='unverified' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	res := call(t, router, "POST", "/api/v1/auth/password/reset", `{"token":"`+token+`","new":"replacement password"}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("old reset credential bypassed mailbox confirmation=%d", res.Code)
	}
	var retained int
	if err := admin.QueryRow("SELECT count(*) FROM password_tokens WHERE token_hash=$1", contracts.Hash(token)).Scan(&retained); err != nil || retained != 1 {
		t.Fatalf("refused reset did not roll back token consumption: count=%d err=%v", retained, err)
	}
}

func TestVerificationResendFailsClosedWhenItsRecipientStoreFails(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup)
	person(t, conn, "known@example.com")
	_, err := admin.Exec(`CREATE FUNCTION reject_verification_counter() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
		IF NEW.key LIKE '%auth/verification-mail/%' THEN RAISE EXCEPTION 'recipient counter unavailable'; END IF;
		RETURN NEW; END $$;
		CREATE TRIGGER reject_verification_counter BEFORE INSERT OR UPDATE ON platformkit_limits
		FOR EACH ROW EXECUTE FUNCTION reject_verification_counter()`)
	if err != nil {
		t.Fatal(err)
	}
	var response string
	for _, email := range []string{"known@example.com", "absent@example.com"} {
		res := call(t, router, "POST", "/api/v1/auth/resend-verification", `{"email":"`+email+`"}`)
		var fault problem.Problem
		if err := json.Unmarshal(res.Body.Bytes(), &fault); err != nil {
			t.Fatal("recipient-store failure did not return problem details")
		}
		// Request instance IDs differ; public problem meaning must not.
		meaning := fault.Type + ":" + fault.Title + ":" + fault.Detail
		if res.Code != http.StatusServiceUnavailable || fault.Status != res.Code || (response != "" && response != meaning) {
			t.Fatalf("recipient-store failure response=%d", res.Code)
		}
		response = meaning
	}
	var queued int
	if err := admin.QueryRow("SELECT count(*) FROM platformkit_outbox WHERE name=$1", contracts.EventVerificationRequested).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("failed recipient store still queued delivery: count=%d err=%v", queued, err)
	}
}

func TestEmailSignupDuplicatesPreserveEveryExistingState(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup)
	var acknowledged string
	for i, status := range []string{user.StatusInvited, user.StatusPending, user.StatusUnverified, user.StatusActive, user.StatusInactive} {
		email := status + "@example.com"
		var original *user.User
		if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			u, err := realUsers().Invite(ctx, tx, email, "Original")
			if err != nil {
				return err
			}
			if err := realUsers().SetPassword(ctx, tx, u.ID, authtest.Password); err != nil {
				return err
			}
			if _, err := realUsers().SetRoles(ctx, tx, u.ID, []string{"admin"}); err != nil {
				return err
			}
			if err := tx.DB().Exec("UPDATE users SET status = ? WHERE id = ?", status, u.ID).Error; err != nil {
				return err
			}
			original, err = realUsers().Get(ctx, tx, u.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		res := call(t, router, "POST", "/api/v1/auth/register", approvalBody(t, strings.ToUpper(email), func(body map[string]any) {
			body["password"], body["confirmation"] = "replacement private passphrase", "replacement private passphrase"
		}), from("203.0.113."+strconv.Itoa(i+10)))
		if res.Code != http.StatusAccepted || len(res.Result().Cookies()) != 0 {
			t.Fatalf("duplicate %s=%d", status, res.Code)
		}
		if acknowledged != "" && acknowledged != res.Body.String() {
			t.Fatal("duplicate acknowledgment exposes account state")
		}
		acknowledged = res.Body.String()
		if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			got, err := realUsers().Get(ctx, tx, original.ID)
			if err == nil && (got.Status != original.Status || got.Email != original.Email || got.DisplayName != original.DisplayName || got.PasswordHash != original.PasswordHash || !slices.Equal(got.Roles, original.Roles)) {
				t.Fatal("duplicate signup changed the original account")
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	res := call(t, router, "POST", "/api/v1/auth/register", approvalBody(t, "new@example.com", nil), from("203.0.113.80"))
	if res.Code != http.StatusAccepted || res.Body.String() != acknowledged {
		t.Fatal("new and existing signup acknowledgments differ")
	}
}

func TestVerificationResendIsNeutralAndReplacesOnlyItsOwnCredential(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	router, _, auths := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup)
	u, first := verificationSignup(t, conn, router, "pending@example.com")
	var acknowledgment string
	for i, email := range []string{"pending@example.com", "absent@example.com", "PENDING@example.com", "ABSENT@example.com"} {
		res := call(t, router, "POST", "/api/v1/auth/resend-verification", `{"email":"`+email+`"}`, from("203.0.113."+strconv.Itoa(i+10)))
		if res.Code != http.StatusAccepted || res.Header().Get("Retry-After") != "" || len(res.Result().Cookies()) != 0 {
			t.Fatalf("neutral resend=%d", res.Code)
		}
		if acknowledgment != "" && acknowledgment != res.Body.String() {
			t.Fatal("resend exposes existence or cooldown")
		}
		acknowledgment = res.Body.String()
	}
	var queued int
	if err := admin.QueryRow("SELECT count(*) FROM platformkit_outbox WHERE name=$1", contracts.EventVerificationRequested).Scan(&queued); err != nil || queued != 2 {
		t.Fatalf("recipient limiter requests=%d err=%v", queued, err)
	}
	worker(t, conn)
	if len(mailbox.Sent()) != 1 {
		t.Fatal("immediate resend bypassed the token cooldown")
	}
	// Advance only disposable fixture timestamps; no wait or changed policy.
	if _, err := admin.Exec("UPDATE verification_tokens SET created_at=created_at-interval '2 minutes' WHERE user_id=$1", u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("DELETE FROM platformkit_limits WHERE key LIKE '%auth/verification-mail/%'"); err != nil {
		t.Fatal(err)
	}
	res := call(t, router, "POST", "/api/v1/auth/resend-verification", `{"email":"pending@example.com"}`, from("203.0.113.80"))
	if res.Code != http.StatusAccepted {
		t.Fatalf("later resend=%d", res.Code)
	}
	worker(t, conn)
	letters := mailbox.Sent()
	if len(letters) != 2 {
		t.Fatalf("later resend count=%d", len(letters))
	}
	second := authtest.TokenIn(letters[1].Body)
	if second == "" || second == first {
		t.Fatal("resend did not rotate the credential")
	}
	if res := call(t, router, "POST", "/api/v1/auth/verify-email", verificationBody(t, first)); res.Code != http.StatusUnauthorized {
		t.Fatalf("old verification after rotation=%d", res.Code)
	}
	// Password setup/reset and mailbox confirmation are separate purposes.
	resetBody := `{"token":"` + second + `","new":"replacement password"}`
	if res := call(t, router, "POST", "/api/v1/auth/password/reset", resetBody); res.Code != http.StatusUnauthorized {
		t.Fatalf("verification credential used for reset=%d", res.Code)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := auths.Reissue(ctx, tx, u.Email); err != nil {
			return err
		}
		return realUsers().SetPassword(ctx, tx, u.ID, "replacement password")
	}); !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("password change bypassed verification: %v", err)
	}
	if len(mailbox.Sent()) != 2 {
		t.Fatal("password recovery emailed an unverified account")
	}
	if res := call(t, router, "POST", "/api/v1/auth/verify-email", verificationBody(t, second)); res.Code != http.StatusOK {
		t.Fatalf("current verification after refused reset=%d", res.Code)
	}
}

type verificationFailingMailer struct {
	fail  bool
	token string
	box   authtest.Mailbox
}

func (m *verificationFailingMailer) Send(ctx context.Context, message notification.Message) error {
	m.token = authtest.TokenIn(message.Body)
	if m.fail {
		return fmt.Errorf("transport echoed its input: %s", message.Body)
	}
	return m.box.Send(ctx, message)
}

func TestVerificationDeliveryFailureRetainsTheRegistrationWithoutPersistingSecrets(t *testing.T) {
	// The sweep below reads every table a secret could have reached, the audit
	// trail among them, so the audit module's schema is composed as well.
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	mailer := &verificationFailingMailer{fail: true}
	router, _, auths := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) { d.Mailer = mailer })
	if res := call(t, router, "POST", "/api/v1/auth/register", approvalBody(t, "delivery@example.com", nil)); res.Code != http.StatusAccepted {
		t.Fatalf("signup=%d", res.Code)
	}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var payload []byte
		if err := tx.DB().Raw("SELECT payload FROM platformkit_outbox WHERE name=?", user.EventRegistrationUnverified).Row().Scan(&payload); err != nil {
			return err
		}
		for _, sub := range subs {
			if sub.Name == user.EventRegistrationUnverified {
				return sub.Handler(ctx, tx, events.Event{Name: sub.Name, TenantID: acme.ID, Payload: payload})
			}
		}
		return nil
	})
	if err == nil || mailer.token == "" || strings.Contains(err.Error(), mailer.token) {
		t.Fatal("delivery failure was lost or exposed a bearer")
	}
	var accounts, tokens int
	if err := admin.QueryRow("SELECT count(*) FROM users WHERE status='unverified'").Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow("SELECT count(*) FROM verification_tokens").Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if accounts != 1 || tokens != 0 {
		t.Fatalf("failed delivery accounts=%d tokens=%d", accounts, tokens)
	}
	mailer.fail = false
	worker(t, conn)
	if len(mailer.box.Sent()) != 1 {
		t.Fatal("queued delivery did not recover")
	}
	for _, table := range []string{"verification_tokens", "password_tokens", "users", "sessions", "notifications", "audit_events", "platformkit_outbox", "platformkit_dead_letters"} {
		for _, secret := range []string{mailer.token, authtest.Password} {
			var count int
			if err := admin.QueryRowContext(t.Context(), fmt.Sprintf("SELECT count(*) FROM %s r WHERE strpos(r::text,$1)>0", table), secret).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("%s persisted a plaintext credential", table)
			}
		}
	}
	if _, err := admin.Exec("UPDATE verification_tokens SET expires_at=clock_timestamp()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		count, err := auths.Purge(ctx, tx)
		if err == nil && count != 1 {
			t.Fatalf("expired verification purge=%d", count)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
