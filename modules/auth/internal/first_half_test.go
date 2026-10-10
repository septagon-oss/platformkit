package internal_test

// The window a refused sign-in leaves behind, held to the two things a window
// must get right: it answers once, and it stops answering on its own.
//
// `second_factor_requires_its_first_half_test.go` pins the refusal — a code with
// no refused door behind it signs nobody in and spends nothing. These two cases
// are the other halves of the same rule, which that case has no reason to look
// at. The first is that the window is *consumed* by the answer that uses it: a
// second credential from the same caller a moment later — no new password, no new
// refusal — is refused, and refused without spending that second credential
// either, so the person keeps what they were refused for. The second is that the
// row is not a permanent mark on the account: it ages out, the hourly sweep that
// already empties `password_tokens` empties it, and while it is still live the
// sweep leaves it alone.
//
// Nothing here names a status code the module could answer some other way, and
// nothing reads a sentence out of a refusal: the assertions are the ledger —
// sessions, unused codes, rows left in one table.
//
// The file carries a third case that arrived from elsewhere. Two tabs answering
// one live step at the challenge route used to be the proof that the replay guard
// is the UPDATE (`WHERE last_step < ?`) rather than a comparison — see
// `review_r5_two_tabs_answering_one_code_test.go`. Since `000033` the tab without
// a refused sign-in behind it is turned away at the door and never reaches that
// statement, so the proof lives where the statement is the only guard left: two
// transactions, one step, no HTTP in sight.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// TestTheWindowIsSpentByTheAnswerThatUsesIt: one refused password, one answer.
func TestTheWindowIsSpentByTheAnswerThatUsesIt(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _, api := mountRecorded(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.FactorKey = "a factor key this deployment set"
	})
	challenge := ""
	for _, m := range api.Mounted() {
		if m.Module == "auth" && m.Method == http.MethodPost && strings.HasSuffix(m.Path, "/challenge/verify") {
			challenge = m.Path
		}
	}
	if challenge == "" {
		t.Fatal("the composition mounts no auth-challenge-verify operation")
	}
	const email = "ada@acme.localhost"
	ada := person(t, conn, email, contracts.RoleAdmin)
	secret, codes := enrolByRoute(t, router, conn, email)

	halted := func() {
		t.Helper()
		if res := call(t, router, http.MethodPost, "/api/v1/auth/login",
			`{"email":"`+email+`","password":"`+authtest.Password+`"}`); res.Code != http.StatusUnauthorized {
			t.Fatalf("the password alone, with a factor enrolled = %d %s, want 401", res.Code, res.Body.String())
		}
	}

	// The window opens and one answer closes it.
	halted()
	if answered := call(t, router, http.MethodPost, challenge,
		`{"email":"`+email+`","code":"`+codeFor(t, secret, db.Now())+`"}`); answered.Code != http.StatusOK {
		t.Fatalf("answering the challenge just after the refusal = %d %s, want 200", answered.Code, answered.Body.String())
	}

	// Where the ledger stands with one signed-in answer behind it: the session
	// the enrolment opened is still there, and the challenge added one.
	opened := proofSessions(t, conn, ada)

	// The same caller, one request later, holding a credential that has never
	// been offered: there is no window left, so this is a refusal — and it is a
	// refusal that may not spend the code it refused, which is what the last
	// line of this case checks.
	_, spareBefore, _ := proofLedger(t, conn, ada)
	second := call(t, router, http.MethodPost, challenge,
		`{"email":"`+email+`","code":"`+codes[1]+`"}`)
	if second.Code == http.StatusOK || second.Code == http.StatusCreated {
		t.Errorf("a recovery code answered with no refused sign-in behind it = %d, want a refusal: the "+
			"answer that opened the session spent the window, and a window that answered twice is a door "+
			"that stays open for whoever held the code", second.Code)
	}
	if sessionCookie(second) != "" {
		t.Error("the second answer set a session cookie")
	}
	live, spare, _ := proofLedger(t, conn, ada)
	if spare != spareBefore {
		t.Errorf("%d recovery codes are unused after the refusal, want the %d held before it", spare, spareBefore)
	}
	if live != opened {
		t.Errorf("%d sessions are live for %s after the refusal, want the %d held before it: a refused "+
			"answer opens no session, whatever it was carrying", live, email, opened)
	}

	// And the refused code still works, once the account is refused again: the
	// refusal spent nothing at all.
	halted()
	if again := call(t, router, http.MethodPost, challenge,
		`{"email":"`+email+`","code":"`+codes[1]+`"}`); again.Code != http.StatusOK {
		t.Fatalf("the same recovery code, answered behind a fresh refusal = %d %s, want 200: a refusal "+
			"that spends a code leaves the account one code poorer for an attempt that got nowhere",
			again.Code, again.Body.String())
	}
	if _, spare, _ = proofLedger(t, conn, ada); spare != spareBefore-1 {
		t.Errorf("%d recovery codes are unused after the spend, want %d", spare, spareBefore-1)
	}
	// The window itself is one row per person, however many times the password
	// was refused: a script against one address cannot grow this table.
	for range 3 {
		halted()
	}
	if rows := proofRows(t, conn, ada); rows != 1 {
		t.Errorf("%d windows are held for %s after four refused sign-ins, want 1: refreshing is what "+
			"keeps a table nobody deletes from from being the one thing an attacker can grow", rows, email)
	}
}

// TestThePurgeTakesAnAgedWindow: five minutes is a lifetime, and a sweep is what
// makes it a lifetime rather than a growing table. This is the same claim
// TestThePurgeTakesExpiredCredentials makes of sessions and reset links.
func TestThePurgeTakesAnAgedWindow(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users := realUsers()
	svc := internal.NewService(users, nil, internal.Delivery{})
	svc.EnableFactors([]byte("a factor key this deployment set"))
	seed(t, conn, acme)
	ctx := httpx.WithConn(t.Context(), conn)
	const email = "ada@acme.example.com"

	err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		person, err := users.Invite(ctx, tx, email, email)
		if err != nil {
			return err
		}
		if err := users.SetPassword(ctx, tx, person.ID, authtest.Password); err != nil {
			return err
		}
		enrolment, err := svc.BeginTOTP(ctx, tx, person.ID)
		if err != nil {
			return err
		}
		_, _, err = svc.FinishTOTP(ctx, tx, person.ID, enrolment.Secret, codeFor(t, enrolment.Secret, db.Now()))
		return err
	})
	if err != nil {
		t.Fatalf("enrol the factor: %v", err)
	}
	if err := refusedSignIn(t, ctx, conn, svc, email); !errors.Is(err, contracts.ErrFactorRequired) {
		t.Fatalf("the password behind a factor = %v, want %v", err, contracts.ErrFactorRequired)
	}

	exec(t, admin, `UPDATE first_factor_proofs SET expires_at = now() - interval '1 minute'`)
	if gone := purge(t, ctx, conn, svc); gone != 1 {
		t.Errorf("Purge took %d rows, want the one aged window", gone)
	}
	if left := countRows(t, admin, "first_factor_proofs"); left != 0 {
		t.Errorf("%d windows are left after the purge took an aged one, want none", left)
	}

	// A fresh window is not the sweep's: it is a live credential of a person
	// mid-sign-in, and the purge would sign them out of their own second step.
	if err := refusedSignIn(t, ctx, conn, svc, email); !errors.Is(err, contracts.ErrFactorRequired) {
		t.Fatalf("the password behind a factor, again = %v, want %v", err, contracts.ErrFactorRequired)
	}
	if gone := purge(t, ctx, conn, svc); gone != 0 {
		t.Errorf("Purge took %d rows, want 0: the window is still open and the person has not answered yet", gone)
	}
	if left := countRows(t, admin, "first_factor_proofs"); left != 1 {
		t.Errorf("%d windows are left after a purge that should have taken nothing, want the 1 still open", left)
	}
}

// refusedSignIn offers the password and takes the refusal, which is the one
// thing that opens a window; the error it hands back is the ErrFactorRequired the
// caller asked for.
func refusedSignIn(t *testing.T, ctx context.Context, conn *db.Conn, svc *internal.Service, email string) error {
	t.Helper()
	var err error
	run := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, _, err = svc.Login(ctx, tx, email, authtest.Password, nobody)
		return nil
	})
	if run != nil {
		t.Fatalf("the refused sign-in: %v", run)
	}
	return err
}

func countRows(t *testing.T, admin *sql.DB, table string) int {
	t.Helper()
	var n int
	row(t, admin, "SELECT count(*) FROM "+table).Scan(&n)
	return n
}

// enrolByRoute signs the person in with a password, enrols a factor over the two
// routes and returns the secret with the codes the enrolment handed out — the
// state a person is in when a door starts refusing them.
func enrolByRoute(t *testing.T, router http.Handler, conn *db.Conn, email string) (secret string, codes []string) {
	t.Helper()
	cookie := sessionCookie(call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`))
	if cookie == "" {
		t.Fatal("the password sign-in that starts an enrolment set no session cookie")
	}
	begin := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/begin", "", withSession(cookie))
	var enrolment contracts.TOTPEnrolment
	if err := json.Unmarshal(begin.Body.Bytes(), &enrolment); err != nil {
		t.Fatalf("read the enrolment: %v (%s)", err, begin.Body.String())
	}
	finish := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/finish",
		`{"secret":"`+enrolment.Secret+`","code":"`+codeFor(t, enrolment.Secret, db.Now())+`"}`,
		withSession(cookie))
	if finish.Code != http.StatusCreated {
		t.Fatalf("finishing the enrolment = %d %s, want 201", finish.Code, finish.Body.String())
	}
	var issued struct {
		Codes []string `json:"codes"`
	}
	if err := json.Unmarshal(finish.Body.Bytes(), &issued); len(issued.Codes) < 2 || err != nil {
		t.Fatalf("the enrolment issued no recovery codes: %s", finish.Body.String())
	}
	return enrolment.Secret, issued.Codes
}

// proofLedger reads what a refused sign-in is not allowed to move: this person's
// live sessions and their unspent recovery codes, plus the trail.
func proofLedger(t *testing.T, conn *db.Conn, person uuid.UUID) (live, unused int, events []string) {
	t.Helper()
	err := db.Run(httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn), conn,
		func(_ context.Context, tx db.Tx[db.Tenant]) error {
			held, err := liveFactorSessions(t, tx, person)
			if err != nil {
				return err
			}
			live = held
			var spare int64
			if err := tx.DB().Table("recovery_codes").Where("used_at IS NULL").Count(&spare).Error; err != nil {
				return err
			}
			unused, events = int(spare), outbox(t, tx)
			return nil
		})
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	return live, unused, events
}

func proofRows(t *testing.T, conn *db.Conn, person uuid.UUID) int {
	t.Helper()
	var rows int64
	err := db.Run(httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn), conn,
		func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Table("first_factor_proofs").Where("user_id = ?", person).Count(&rows).Error
		})
	if err != nil {
		t.Fatalf("count the windows: %v", err)
	}
	return int(rows)
}

// proofSessions counts the sessions Identify would still take for this person.
func proofSessions(t *testing.T, conn *db.Conn, person uuid.UUID) int {
	t.Helper()
	var live int
	err := db.Run(httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn), conn,
		func(_ context.Context, tx db.Tx[db.Tenant]) error {
			held, err := liveFactorSessions(t, tx, person)
			live = held
			return err
		})
	if err != nil {
		t.Fatalf("count the sessions: %v", err)
	}
	return live
}

// TestTwoCallersSpendingOneStepOpenOneSession: one RFC 6238 step, two
// transactions, one session. Both callers are past every door — the spend is
// decided by the row, and the row has to refuse one of them.
//
// What it pins is the outcome review 5 asked for, and no more than that. The
// losing caller is refused either by the guarded UPDATE, when it reaches a row the
// winner has already written, or by the comparison in totpMatches a line earlier,
// when it read the row before that write landed; which of the two happens is the
// interleaving, and no case in this tree can force one. Drop the WHERE last_step <
// ? and this still passes — the read carries the refusal instead — so the case
// rules out a spend that lets a step answer twice, and says nothing about which of
// the two statements is the one that stopped it. That is stated here rather than
// claimed: the browser case this replaces made the stronger claim by accident of
// its timing, not by design.
func TestTwoCallersSpendingOneStepOpenOneSession(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users := realUsers()
	svc := internal.NewService(users, nil, internal.Delivery{})
	svc.EnableFactors([]byte("a factor key this deployment set"))
	seed(t, conn, acme)
	ctx := httpx.WithConn(t.Context(), conn)
	const email = "ada@acme.example.com"

	var secret string
	err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		person, err := users.Invite(ctx, tx, email, email)
		if err != nil {
			return err
		}
		if err := users.SetPassword(ctx, tx, person.ID, authtest.Password); err != nil {
			return err
		}
		enrolment, err := svc.BeginTOTP(ctx, tx, person.ID)
		if err != nil {
			return err
		}
		_, _, err = svc.FinishTOTP(ctx, tx, person.ID, enrolment.Secret, codeFor(t, enrolment.Secret, db.Now()))
		secret = enrolment.Secret
		return err
	})
	if err != nil {
		t.Fatalf("enrol the factor: %v", err)
	}
	code := codeFor(t, secret, db.Now())

	outcomes := make([]error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range outcomes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			outcomes[i] = db.Run(tenancy.WithTenant(ctx, acme), conn,
				func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					_, _, err := svc.VerifySecondFactor(ctx, tx, email, code, nobody)
					return err
				})
		}()
	}
	close(start)
	wg.Wait()

	opened := 0
	for _, err := range outcomes {
		switch {
		case err == nil:
			opened++
		case errors.Is(err, contracts.ErrCredentials):
		default:
			t.Fatalf("a caller spending one step = %v, want either the session or %v", err, contracts.ErrCredentials)
		}
	}
	if opened != 1 {
		t.Errorf("%d of 2 callers spent one live step, want exactly one: last_step is the replay refusal "+
			"and the replay record in one statement, so a second transaction that also opens a session read "+
			"a step the first had already taken (%v)", opened, outcomes)
	}
	var live, logins int64
	err = db.Run(httpx.WithConn(tenancy.WithTenant(context.Background(), acme), conn), conn,
		func(_ context.Context, tx db.Tx[db.Tenant]) error {
			if err := tx.DB().Table("sessions").Where("expires_at > ?", db.Now()).Count(&live).Error; err != nil {
				return err
			}
			return tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventLoggedIn).
				Count(&logins).Error
		})
	if err != nil {
		t.Fatalf("read what the two callers left: %v", err)
	}
	if live != 1 || logins != 1 {
		t.Errorf("%d live sessions and %d %s rows for one step spent twice, want one and one",
			live, logins, contracts.EventLoggedIn)
	}
}

func purge(t *testing.T, ctx context.Context, conn *db.Conn, svc *internal.Service) int {
	t.Helper()
	var gone int64
	err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		gone, err = svc.Purge(ctx, tx)
		return err
	})
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	return int(gone)
}
