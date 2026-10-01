package internal_test

// review 5 (decision 0039: HIGHs only). The one claim about the second factor and
// the bearer key that nobody has ever run.
//
// Both are spent by a guarded UPDATE — `WHERE last_step < ?` on the factor row,
// `WHERE revoked_at IS NULL` on the key row — and the module says the guard *is*
// the refusal: "the factor row remembers the highest step it accepted (last_step)
// and the spend is UPDATE … WHERE last_step < ?, which is the refusal and the
// record in one statement". The sessions table has a two-tab case for exactly
// this shape (`TestTwoTabsRevokingOneSessionSettleOnce`). No case anywhere runs
// two concurrent spends against a factor row or a token row:
//
//	$ grep -rn "sync.WaitGroup" modules/auth/internal/*_test.go
//	  sessions_test.go  roles_concurrency_test.go  verification_atomic_test.go
//
// and the last delivery commit's own `Not verified:` line repeats it — "two
// concurrent spends against one factor row or one token row, which no case in the
// tree runs". A guard that is only argued in a comment is the thing a second
// request reads the row for, checks in Go, and then writes. So: two requests, one
// live code, one answer that must be a session.
//
// This case has a passing branch that is just the correct behaviour — one 200 with
// a cookie, the other refused, one session row, one logged_in — and it reaches the
// refusal through the status a working refusal carries (401) and the row it
// leaves, never through any sentence the refusal prints.

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// TestTwoTabsAnsweringOneCodeOpenOneSession is the factor spend.
func TestTwoTabsAnsweringOneCodeOpenOneSession(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.FactorKey = "a factor key this deployment set"
	})
	const email = "ada@acme.localhost"
	person(t, conn, email, contracts.RoleAdmin)

	res := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("password sign-in = %d %s, want 200", res.Code, res.Body.String())
	}
	cookie := sessionCookie(res)
	begin := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/begin", "", withSession(cookie))
	var enrolment contracts.TOTPEnrolment
	if err := json.Unmarshal(begin.Body.Bytes(), &enrolment); err != nil {
		t.Fatalf("read the enrolment: %v (%s)", err, begin.Body.String())
	}
	code := codeFor(t, enrolment.Secret, db.Now())
	if finish := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/finish",
		`{"secret":"`+enrolment.Secret+`","code":"`+code+`"}`, withSession(cookie)); finish.Code != http.StatusCreated {
		t.Fatalf("finishing the enrolment = %d %s, want 201", finish.Code, finish.Body.String())
	}

	// One live step, two requests, no gap for a replay window to open in: both
	// compute the code for the same instant and go at the same moment. The counts
	// are taken first and read as a delta, because this person already holds the
	// session they enrolled with and the trail already names that login.
	var sessionsBefore, loginsBefore int64
	ctx := tenancy.WithTenant(t.Context(), acme)
	count := func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Table("sessions").Where("user_id = (SELECT id FROM users WHERE email = ?)", email).
			Count(&sessionsBefore).Error; err != nil {
			return err
		}
		return tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventLoggedIn).
			Count(&loginsBefore).Error
	}
	if err := db.Run(ctx, conn, count); err != nil {
		t.Fatalf("count before the attempt: %v", err)
	}
	var sessions, logins int64

	attempts := make([]int, 2)
	cookies := make([]string, 2)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := call(t, router, http.MethodPost, "/api/v1/auth/challenge/verify",
				`{"email":"`+email+`","code":"`+codeFor(t, enrolment.Secret, db.Now())+`"}`)
			attempts[i], cookies[i] = res.Code, sessionCookie(res)
		}()
	}
	wg.Wait()

	opened, refused := 0, 0
	for i, code := range attempts {
		switch {
		case code == http.StatusOK && cookies[i] != "":
			opened++
		case code == http.StatusUnauthorized && cookies[i] == "":
			refused++
		default:
			t.Fatalf("an attempt to answer the second factor = %d with cookie %q, want either a 200 "+
				"that sets a cookie or a 401 that sets none (attempts %v)", code, cookies[i], attempts)
		}
	}
	if opened != 1 || refused != 1 {
		t.Errorf("%d sessions and %d refusals for one live code spent twice at once, want exactly one of "+
			"each — the factor spend is one statement, so a second request that also opens a session is a "+
			"replay of a step the module says it refuses", opened, refused)
	}

	// What the trail says about it: one login, however many requests asked.
	err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Table("sessions").Where("user_id = (SELECT id FROM users WHERE email = ?)", email).
			Count(&sessions).Error; err != nil {
			return err
		}
		return tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventLoggedIn).
			Count(&logins).Error
	})
	if err != nil {
		t.Fatalf("count what the two attempts wrote: %v", err)
	}
	if got := sessions - sessionsBefore; got != 1 {
		t.Errorf("the sessions table grew by %d rows for one code spent twice, want 1", got)
	}
	if got := logins - loginsBefore; got != 1 {
		t.Errorf("the outbox holds %d %s events for one code spent twice, want 1", got, contracts.EventLoggedIn)
	}
}

// TestTwoTabsSpendingOneRecoveryCodeSpendItOnce is the same question at the row
// whose guard is `used_at IS NULL`. The recovery code is the credential a person
// keeps in a drawer: two of them live, so a double-spend is a code that worked
// twice rather than a step that was replayed.
func TestTwoTabsSpendingOneRecoveryCodeSpendItOnce(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.FactorKey = "a factor key this deployment set"
	})
	const email = "ada@acme.localhost"
	person(t, conn, email, contracts.RoleAdmin)
	res := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`)
	cookie := sessionCookie(res)
	begin := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/begin", "", withSession(cookie))
	var enrolment contracts.TOTPEnrolment
	if err := json.Unmarshal(begin.Body.Bytes(), &enrolment); err != nil {
		t.Fatalf("read the enrolment: %v (%s)", err, begin.Body.String())
	}
	finish := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/finish",
		`{"secret":"`+enrolment.Secret+`","code":"`+codeFor(t, enrolment.Secret, db.Now())+`"}`, withSession(cookie))
	var codes struct {
		Codes []string `json:"codes"`
	}
	if err := json.Unmarshal(finish.Body.Bytes(), &codes); err != nil || len(codes.Codes) == 0 {
		t.Fatalf("the enrolment issued no recovery codes: %d %v %s", finish.Code, err, finish.Body.String())
	}
	one := codes.Codes[0]

	var spentBefore int64
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("recovery_codes").Where("used_at IS NOT NULL").Count(&spentBefore).Error
	}); err != nil {
		t.Fatalf("count before the attempt: %v", err)
	}

	attempts := make([]int, 2)
	got := make([]string, 2)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := call(t, router, http.MethodPost, "/api/v1/auth/challenge/verify",
				`{"email":"`+email+`","code":"`+one+`"}`)
			attempts[i], got[i] = res.Code, sessionCookie(res)
		}()
	}
	wg.Wait()

	opened, refused := 0, 0
	for i, code := range attempts {
		switch {
		case code == http.StatusOK && got[i] != "":
			opened++
		case code == http.StatusUnauthorized && got[i] == "":
			refused++
		default:
			t.Fatalf("an attempt to spend a recovery code = %d with cookie %q, want a 200 with a cookie "+
				"or a 401 with none (attempts %v)", code, got[i], attempts)
		}
	}
	if opened != 1 || refused != 1 {
		t.Errorf("%d sessions and %d refusals for one recovery code spent twice at once, want exactly one "+
			"of each — %v", opened, refused, attempts)
	}

	var spent int64
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("recovery_codes").Where("used_at IS NOT NULL").Count(&spent).Error
	}); err != nil {
		t.Fatalf("count the spent codes: %v", err)
	}
	if got := spent - spentBefore; got != 1 {
		t.Errorf("%d recovery codes were marked spent for one code presented twice, want 1", got)
	}
}
