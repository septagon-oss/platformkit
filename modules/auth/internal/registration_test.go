package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestRegistrationIsOptIn(t *testing.T) {
	router, _, _ := mount(t, auth.OIDC{})
	res := call(t, router, http.MethodPost, "/api/v1/auth/register", `{"email":"student@example.com"}`)
	if res.Code != http.StatusNotFound {
		t.Fatalf("disabled registration = %d", res.Code)
	}
}

func TestRegistrationCreatesOnlyAnInvitedMemberAfterTheRequest(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, true)
	res := call(t, router, http.MethodPost, "/api/v1/auth/register", `{"email":"Student@EXAMPLE.com","displayName":"Student","roles":["admin"]}`)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("caller-supplied roles = %d %s", res.Code, res.Body)
	}
	res = call(t, router, http.MethodPost, "/api/v1/auth/register", `{"email":"Student@EXAMPLE.com","displayName":"Student"}`)
	if res.Code != http.StatusAccepted {
		t.Fatalf("registration = %d %s", res.Code, res.Body)
	}
	var before int
	if err := admin.QueryRow("SELECT count(*) FROM users").Scan(&before); err != nil || before != 0 {
		t.Fatalf("request performed account lookup/creation: users=%d, err=%v", before, err)
	}
	if strings.Contains(res.Body.String(), "student") || strings.Contains(res.Body.String(), "token") {
		t.Fatal("the acknowledgment exposes account information")
	}
	registrationWorker(t, conn, acme)
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := realUsers().ByEmail(ctx, tx, "student@example.com")
		if err != nil {
			return err
		}
		if u.Status != user.StatusInvited || u.PasswordHash != "" || len(u.Roles) != 1 || u.Roles[0] != contracts.RoleMember {
			t.Errorf("registration did not create an unverified member: status=%s roles=%v", u.Status, u.Roles)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	worker(t, conn)
	if len(mailbox.Sent()) != 1 {
		t.Fatalf("password setup messages = %d", len(mailbox.Sent()))
	}
	res = call(t, router, http.MethodPost, "/api/v1/auth/login", `{"email":"student@example.com","password":"`+authtest.Password+`"}`)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unverified account sign-in = %d", res.Code)
	}
	token := authtest.TokenIn(mailbox.Sent()[0].Body)
	reset := `{"token":"` + token + `","new":"` + authtest.Password + `"}`
	res = call(t, router, http.MethodPost, "/api/v1/auth/password/reset", reset)
	if res.Code != http.StatusOK {
		t.Fatalf("password setup = %d %s", res.Code, res.Body)
	}
	res = call(t, router, http.MethodPost, "/api/v1/auth/login", `{"email":"student@example.com","password":"`+authtest.Password+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("verified member sign-in = %d %s", res.Code, res.Body)
	}
	res = call(t, router, http.MethodPost, "/api/v1/auth/password/reset", reset)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("spent setup token = %d", res.Code)
	}
}

func TestRegistrationPreservesExistingAccountsAndIsTenantScoped(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, true)
	id := person(t, conn, "student@example.com", contracts.RoleAdmin)
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := realUsers().Deactivate(ctx, tx, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		res := call(t, router, http.MethodPost, "/api/v1/auth/register", `{"email":"student@example.com","displayName":"Changed"}`)
		if res.Code != http.StatusAccepted {
			t.Fatalf("repeat registration = %d %s", res.Code, res.Body)
		}
	}
	registrationWorker(t, conn, acme)
	registrationWorker(t, conn, acme)
	var status, name string
	if err := admin.QueryRow("SELECT status, display_name FROM users WHERE id = $1", id).Scan(&status, &name); err != nil {
		t.Fatal(err)
	}
	if status != user.StatusInactive || name == "Changed" {
		t.Fatalf("existing account changed: status=%s name=%q", status, name)
	}
	seed(t, conn, globex)
	payload, err := json.Marshal(contracts.RegistrationRequested{Email: "student@example.com", DisplayName: "Other tenant"})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, sub := range subs {
			if sub.Name == contracts.EventRegistrationRequested {
				return sub.Handler(ctx, tx, events.Event{TenantID: globex.ID, Payload: payload})
			}
		}
		t.Fatal("registration subscriber missing")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := admin.QueryRow("SELECT count(*) FROM users WHERE email = 'student@example.com'").Scan(&count); err != nil || count != 2 {
		t.Fatalf("tenant-local registrations = %d, err=%v", count, err)
	}
}

func TestRegistrationSharesThePublicMailRequestLimit(t *testing.T) {
	_, conn := dbtest.Schema(t)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, true)
	for i := range contracts.ResetRequests + 1 {
		res := call(t, router, http.MethodPost, "/api/v1/auth/register", `{"email":"student@example.com"}`, from("203.0.113.29"))
		want := http.StatusAccepted
		if i == contracts.ResetRequests {
			want = http.StatusTooManyRequests
		}
		if res.Code != want {
			t.Fatalf("request %d = %d, want %d: %s", i+1, res.Code, want, res.Body)
		}
	}
}

func TestConcurrentRegistrationCreatesOneAccount(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	mountConfigured(t, conn, auth.OIDC{}, true)
	payload, err := json.Marshal(contracts.RegistrationRequested{Email: "race@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	var handler events.Handler
	for _, sub := range subs {
		if sub.Name == contracts.EventRegistrationRequested {
			handler = sub.Handler
		}
	}
	if handler == nil {
		t.Fatal("registration subscriber missing")
	}
	results := make(chan error, 4)
	var workers sync.WaitGroup
	for range cap(results) {
		workers.Go(func() {
			results <- db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return handler(ctx, tx, events.Event{TenantID: acme.ID, Payload: payload})
			})
		})
	}
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := admin.QueryRow("SELECT count(*) FROM users WHERE email = 'race@example.com'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("concurrent registrations = %d, err=%v", count, err)
	}
}

// Deliberately replay registration events. The production consumer also claims
// delivery, but account creation must tolerate separate requests and retries.
func registrationWorker(t *testing.T, conn *db.Conn, tenant tenancy.Tenant) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var pending []struct{ Payload []byte }
		if err := tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventRegistrationRequested).Find(&pending).Error; err != nil {
			return err
		}
		for _, event := range pending {
			for _, sub := range subs {
				if sub.Name == contracts.EventRegistrationRequested {
					if err := sub.Handler(ctx, tx, events.Event{TenantID: tenant.ID, Payload: event.Payload}); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
