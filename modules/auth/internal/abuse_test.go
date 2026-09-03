package internal_test

// The properties a public authentication surface is judged by, each with the
// measurement that caught it missing: the soft delay must not hold a database
// connection, a reset link must not be in any row, the forgotten-password route
// must cost the same whoever asks, and neither it nor the lockout may be an
// unbounded source of mail or of audit rows.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// from sets the address a request appears to come from, which is what both
// limiters count. It is RemoteAddr and not a header, because a header a client
// can write is not an address — see internal.ClientOf.
func from(addr string) func(*http.Request) {
	return func(r *http.Request) { r.RemoteAddr = addr + ":50000" }
}

// TestTheSoftDelayHoldsNoTransaction is the one that blocked E7.
//
// An account under a distributed attack earns two seconds (contracts.SoftDelay)
// rather than a refusal, because refusing it is how one stranger locks somebody
// out of their own account. Those two seconds used to be spent inside the
// request's transaction: twenty-four delayed logins from one address held
// sixteen of a replica's seventeen connections, a legitimate request waited
// twenty-nine seconds, and the periodic jobs starved. Nothing about the delay
// needs a database — the limiter is a map in this process — so the pause
// belongs before the transaction is opened.
//
// It is measured from outside, through pg_stat_activity, because the claim is
// about a server-side connection and not about a Go value: the pool is named
// with an application_name of this test's own, and nothing of it may be idle in
// a transaction while the request sleeps.
func TestTheSoftDelayHoldsNoTransaction(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	router, _, _ := mountOn(t, conn, auth.OIDC{})
	person(t, conn, "ada@acme.localhost")

	// Ten failures against one account from four addresses, which is what the
	// limiter calls somebody attacking the lockout rather than guessing a
	// password — so the next attempt is delayed and not refused.
	for i := range contracts.MaxAttempts {
		res := call(t, router, http.MethodPost, "/api/v1/auth/login",
			`{"email":"ada@acme.localhost","password":"the wrong passphrase"}`,
			from(fmt.Sprintf("203.0.113.%d", i%4+1)))
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d = %d %s, want 401", i+1, res.Code, res.Body)
		}
	}

	// This test's own backends and nobody else's: every test in the suite
	// shares one database, and dbtest names each pool after the schema it
	// works in. What is measured is the age of the oldest open transaction and
	// not how many there are: the limiter and the failed-login record write
	// through short detached transactions on this same pool, and a sample can
	// land between two of their statements. A transaction that has been open
	// for half a second is one the sleeping request is holding; nothing else
	// on this pool lives that long.
	longest := func() float64 {
		var age float64
		const q = `SELECT COALESCE(EXTRACT(EPOCH FROM max(now() - xact_start)), 0) FROM pg_stat_activity
			WHERE state = 'idle in transaction' AND application_name = current_setting('search_path')`
		if err := admin.QueryRowContext(t.Context(), q).Scan(&age); err != nil {
			t.Errorf("read pg_stat_activity: %v", err)
		}
		return age
	}

	done := make(chan *httptest.ResponseRecorder, 1)
	start := time.Now()
	go func() {
		done <- call(t, router, http.MethodPost, "/api/v1/auth/login",
			`{"email":"ada@acme.localhost","password":"`+authtest.Password+`"}`,
			from("203.0.113.9"))
	}()

	// While it waits it holds nothing. Sampled over the first three quarters of
	// the delay, so the loop is finished well before the handler wakes up and
	// legitimately opens one.
	samples, oldest := 0, 0.0
	for time.Since(start) < contracts.SoftDelay*3/4 {
		oldest = max(oldest, longest())
		samples++
		time.Sleep(20 * time.Millisecond)
	}
	if samples < 10 {
		t.Fatalf("only %d samples were taken during the delay", samples)
	}
	if oldest > 0.5 {
		t.Errorf("the sleeping request held a transaction open for %.2fs of its delay", oldest)
	}

	res, took := <-done, time.Since(start)
	if res.Code != http.StatusOK {
		t.Fatalf("the delayed login = %d %s, want 200", res.Code, res.Body)
	}
	// And it is still a delay: moving it must not have deleted it.
	if took < contracts.SoftDelay {
		t.Errorf("the login took %s; contracts.SoftDelay is %s and still applies", took, contracts.SoftDelay)
	}
}

// TestTheResetTokenIsInTheMailAndInNoRow.
//
// It used to be in notifications.link, which is an ordinary tenant-owned row: a
// route lists it, it is kept until somebody deletes it, and anybody who can
// read the table can read it. That made every reset link a live credential
// sitting in a table nobody treats as a credential store, and it contradicted
// the property migrations/000014 states in its first paragraph.
//
// The event cannot carry it either — an outbox row is kept for a week and
// modules/audit copies every payload into the audit trail — which is why the
// module that mints the token is the module that hands it to the mail server.
//
// The real notification module is wired here rather than the suite's recorder,
// because the claim is about rows and a recorder writes none. Every table this
// application has is then searched, whole rows cast to text, because the
// interesting failure is the column nobody thought of.
func TestTheResetTokenIsInTheMailAndInNoRow(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	users := realUsers()
	notify, _ := notification.Module(notification.Deps{Mailer: notification.NewMailbox()})
	box := &authtest.Mailbox{}
	svc := internal.NewService(users, notify, delivery(box), operatorPermissions)
	seed(t, conn, svc, acme)
	ctx := httpx.WithConn(t.Context(), conn)

	err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := users.Invite(ctx, tx, "ada@acme.localhost", "Ada")
		if err != nil {
			return err
		}
		if err := users.SetPassword(ctx, tx, u.ID, authtest.Password); err != nil {
			return err
		}
		// The public route's half, and then the worker's.
		if err := svc.Forget(ctx, tx, "ada@acme.localhost"); err != nil {
			return err
		}
		return svc.Reissue(ctx, tx, "ada@acme.localhost")
	})
	if err != nil {
		t.Fatalf("ask for a reset link: %v", err)
	}

	sent := box.Sent()
	if len(sent) != 1 {
		t.Fatalf("%d messages were sent, want one", len(sent))
	}
	token := authtest.TokenIn(sent[0].Body)
	if token == "" {
		t.Fatal("the message carries no token, so there is nothing to look for")
	}
	for _, table := range []string{
		"notifications", "platformkit_outbox", "platformkit_dead_letters",
		"audit_events", "password_tokens", "sessions", "users", "roles",
	} {
		var n int
		// strpos and not LIKE: the token is base64url, so it can hold an
		// underscore, which LIKE would read as a wildcard.
		q := fmt.Sprintf(`SELECT count(*) FROM %s r WHERE strpos(r::text, $1) > 0`, table)
		if err := admin.QueryRowContext(t.Context(), q, token).Scan(&n); err != nil {
			t.Fatalf("search %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%d row(s) of %s carry the token in cleartext", n, table)
		}
	}
	// The hash is there, which is what makes the link usable at all, and the
	// notice a person sees points at the route and carries nothing.
	var byHash, notices int
	row(t, admin, `SELECT count(*) FROM password_tokens WHERE token_hash = $1`,
		contracts.Hash(token)).Scan(&byHash)
	row(t, admin, `SELECT count(*) FROM notifications WHERE link = $1`, internal.ResetPath).Scan(&notices)
	if byHash != 1 || notices != 1 {
		t.Errorf("%d rows hold the token's hash and %d notices point at %s; want one and one",
			byHash, notices, internal.ResetPath)
	}
}

// TestTheForgottenPasswordRouteCostsTheSameEitherWay.
//
// The route's own description says an address nobody has and an address
// somebody has are the same answer. They were not the same answer: the body
// matched and the clock did not — 2.1 ms for a known address against 0.9 ms for
// an unknown one, two distributions that did not overlap at all, which is an
// account enumeration oracle anybody with a stopwatch could run.
//
// They cost the same now because the request does the same thing either way:
// one INSERT into the outbox, and the lookup that used to be here happens in
// the worker. The samples are interleaved so that a busy machine moves both.
func TestTheForgottenPasswordRouteCostsTheSameEitherWay(t *testing.T) {
	if testing.Short() {
		t.Skip("a timing comparison needs samples")
	}
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost")

	const samples = 20
	sample := func(email string, i int) time.Duration {
		// A fresh address each time, because this route has a per-address cap
		// and forty requests from one would be refused half way through.
		at := time.Now()
		res := call(t, router, http.MethodPost, "/api/v1/auth/password/forgot",
			`{"email":"`+email+`"}`, from(fmt.Sprintf("198.51.100.%d", i)))
		took := time.Since(at)
		if res.Code != http.StatusOK {
			t.Fatalf("forgot(%s) = %d %s", email, res.Code, res.Body)
		}
		return took
	}
	var known, unknown []time.Duration
	for i := range samples {
		known = append(known, sample("ada@acme.localhost", i*2))
		unknown = append(unknown, sample("nobody@acme.localhost", i*2+1))
	}
	slices.Sort(known)
	slices.Sort(unknown)
	km, um := known[samples/2], unknown[samples/2]
	t.Logf("known:   median %s, %s … %s", km, known[0], known[samples-1])
	t.Logf("unknown: median %s, %s … %s", um, unknown[0], unknown[samples-1])

	if ratio := float64(max(km, um)) / float64(min(km, um)); ratio > 1.25 {
		t.Errorf("the medians differ by %.2fx (%s against %s); an address somebody has is measurably different",
			ratio, km, um)
	}
	if known[samples-1] < unknown[0] || unknown[samples-1] < known[0] {
		t.Errorf("the two ranges do not overlap: %s…%s against %s…%s",
			known[0], known[samples-1], unknown[0], unknown[samples-1])
	}
}

// TestOneAddressCannotAskForUnboundedMail. Twenty-three requests were
// twenty-three mails to somebody who asked for none, and forty-six audit rows,
// which is a mail relay's reputation spent by a stranger with a browser.
//
// Two caps, and they are different caps. The route's is on the address making
// the request, and it can never be on the address asked about — that would
// answer the question the route exists not to answer. The recipient's is in the
// worker, on how often one person is written to at all.
func TestOneAddressCannotAskForUnboundedMail(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost")

	forgot := func(addr string) int {
		return call(t, router, http.MethodPost, "/api/v1/auth/password/forgot",
			`{"email":"ada@acme.localhost"}`, from(addr)).Code
	}
	for i := range contracts.ResetRequests {
		if got := forgot("198.51.100.7"); got != http.StatusOK {
			t.Fatalf("request %d = %d, want 200", i+1, got)
		}
	}
	if got := forgot("198.51.100.7"); got != http.StatusTooManyRequests {
		t.Errorf("request %d = %d, want 429", contracts.ResetRequests+1, got)
	}
	// Another address is untouched, because the cap is on the caller and Ada is
	// not the one being limited.
	if got := forgot("198.51.100.8"); got != http.StatusOK {
		t.Errorf("a second address = %d, want 200", got)
	}

	// And the worker writes to Ada once, not eleven times: asking again
	// replaces the link and does not add a mail.
	worker(t, conn)
	if got := mailbox.Sent(); len(got) != 1 {
		t.Errorf("%d messages reached Ada, want one", len(got))
	}
	if got := notices.Sent(); len(got) != 1 {
		t.Errorf("%d notices reached Ada, want one", len(got))
	}
}

// TestARefusalIsRecordedOncePerWindow. A locked-out account under a script is
// refused before anything is checked, so recording each refusal was an audit
// row per attempt for a fact that does not change: the first one says the
// account is under attack and the nine hundredth says it still is.
//
// The failures themselves are still recorded one for one — those are attempts
// that were actually made, and the count is what an incident is reconstructed
// from.
func TestARefusalIsRecordedOncePerWindow(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	router, _, _ := mountOn(t, conn, auth.OIDC{})
	person(t, conn, "ada@acme.localhost")

	wrong := func() int {
		return call(t, router, http.MethodPost, "/api/v1/auth/login",
			`{"email":"ada@acme.localhost","password":"the wrong passphrase"}`,
			from("203.0.113.5")).Code
	}
	for i := range contracts.MaxAttempts {
		if got := wrong(); got != http.StatusUnauthorized {
			t.Fatalf("failure %d = %d, want 401", i+1, got)
		}
	}
	if got := failures(t, admin); got != contracts.MaxAttempts {
		t.Fatalf("%d failures were recorded, want %d — every attempt that was made",
			got, contracts.MaxAttempts)
	}

	for i := range 20 {
		if got := wrong(); got != http.StatusTooManyRequests {
			t.Fatalf("refusal %d = %d, want 429", i+1, got)
		}
	}
	if got := failures(t, admin); got != contracts.MaxAttempts+1 {
		t.Errorf("twenty refusals added %d rows, want one", got-contracts.MaxAttempts)
	}
}

// failures is how many auth.login_failed events are in the outbox. They are
// there rather than in the request's transaction because a 401 rolls back — see
// internal.Service.recordFailure.
func failures(t *testing.T, admin *sql.DB) int {
	t.Helper()
	var n int
	row(t, admin, `SELECT count(*) FROM platformkit_outbox WHERE name = $1`,
		contracts.EventLoginFailed).Scan(&n)
	return n
}

// TestAnExpiredSessionGoesOnTheRefusalItCauses.
//
// The delete was in the request's transaction, and a request whose caller was
// not recognised answers 403 — a status kit/httpx rolls back — so the row
// survived every visit it refused and waited up to an hour for the sweep. The
// comment beside it claimed the opposite. It is detached now, so the row goes
// on the first refusal and the sweep is a backstop rather than the mechanism.
func TestAnExpiredSessionGoesOnTheRefusalItCauses(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	router, _, _ := mountOn(t, conn, auth.OIDC{})
	person(t, conn, "ada@acme.localhost")
	session := signIn(t, router, "ada@acme.localhost")
	exec(t, admin, `UPDATE sessions SET expires_at = now() - interval '1 day'`)

	res := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(session))
	if res.Code != http.StatusForbidden {
		t.Fatalf("an expired session = %d %s, want 403", res.Code, res.Body)
	}
	var left int
	row(t, admin, `SELECT count(*) FROM sessions`).Scan(&left)
	if left != 0 {
		t.Errorf("%d expired session(s) survived the refusal they caused", left)
	}
}

// TestTheLockoutIsOneCounterForEveryReplica is what kit/limit bought.
//
// The counters used to be a map in one process: three pods were three lockouts,
// so an attacker got MaxAttempts times the replica count and got it all back on
// every deploy. Here two pools on one database are two pods, the ten failures
// are split between them, and neither will take the eleventh — which is a test
// that fails on any counter that lives in a process.
func TestTheLockoutIsOneCounterForEveryReplica(t *testing.T) {
	adminURL, appURL := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pods := []*db.Conn{pool(t, appURL), pool(t, appURL)}
	services := []contracts.Service{service(t), service(t)}
	person(t, pods[0], "ada@acme.localhost")

	login := func(pod int, password string) error {
		t.Helper()
		conn := pods[pod]
		ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
		var attempt error
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, _, attempt = services[pod].Login(ctx, tx, "ada@acme.localhost", password, nobody)
			return nil
		}); err != nil {
			t.Fatalf("the transaction: %v", err)
		}
		return attempt
	}

	for i := range contracts.MaxAttempts {
		if err := login(i%len(pods), "the wrong passphrase"); !errors.Is(err, contracts.ErrCredentials) {
			t.Fatalf("failure %d on pod %d = %v, want ErrCredentials", i+1, i%len(pods), err)
		}
	}
	// Both of them refuse, and they refuse the correct password: five failures
	// each is what one pod would still be letting through.
	for pod := range pods {
		if err := login(pod, authtest.Password); !errors.Is(err, contracts.ErrTooManyAttempts) {
			t.Errorf("the correct password on pod %d after %d failures spread over %d pods = %v, want ErrTooManyAttempts",
				pod, contracts.MaxAttempts, len(pods), err)
		}
	}
}

// pool is one pod's connection pool, on a schema somebody else migrated.
func pool(t *testing.T, appURL string) *db.Conn {
	t.Helper()
	conn, err := db.Open(t.Context(), appURL)
	if err != nil {
		t.Fatalf("open a pod's pool: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// service is one pod's auth service: its own value, its own everything, the way
// a second process has its own.
func service(t *testing.T) contracts.Service {
	t.Helper()
	return internal.NewService(realUsers(), &authtest.Notices{}, delivery(&authtest.Mailbox{}), operatorPermissions)
}

// TestTheLimiterCountsAnAddressWithoutStoringIt. platformkit_limits is an
// ordinary table with an ordinary backup, and a key of
// "auth/account/ada@acme.localhost" made it a list of every address that has
// ever failed a sign-in here: readable by the owner role, by whoever restores a
// dump, and by anybody who gets a look at either. The counter needs the address
// to be the same string twice and nothing else, which is what a hash is.
func TestTheLimiterCountsAnAddressWithoutStoringIt(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	router, _, _ := mountOn(t, conn, auth.OIDC{})
	const email = "ada@acme.localhost"
	person(t, conn, email)

	for range 3 {
		res := call(t, router, http.MethodPost, "/api/v1/auth/login",
			`{"email":"`+email+`","password":"the wrong passphrase"}`, from("203.0.113.7"))
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("a wrong password = %d %s, want 401", res.Code, res.Body.String())
		}
	}

	rows, err := admin.QueryContext(t.Context(), `SELECT key FROM platformkit_limits`)
	if err != nil {
		t.Fatalf("read the counters: %v", err)
	}
	defer rows.Close()
	counted := 0
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatalf("scan a key: %v", err)
		}
		counted++
		if strings.Contains(key, email) || strings.Contains(key, "ada") || strings.Contains(key, "@") {
			t.Errorf("the counter key %q spells the address it counts", key)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the counters: %v", err)
	}
	if counted == 0 {
		t.Fatal("nothing was counted, so nothing was proved about how it was counted")
	}
}
